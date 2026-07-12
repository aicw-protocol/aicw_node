package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fystack/mpcium/pkg/event"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/types"
)

// reshareMaxTries is the number of attempts per key family within a single
// reconcile pass. On a "new session error" the design retries once with the
// SAME committee (auto_reshare_design.md §7.1) — each try uses a fresh session
// ID (§6.5).
const reshareMaxTries = 2

// ReshareClient is the subset of the mpcium client used by the publisher.
type ReshareClient interface {
	Resharing(msg *types.ResharingMessage) error
	ResharingWithAuthorizers(msg *types.ResharingMessage, collect func(authorizerRaw []byte) ([]types.AuthorizerSignature, error)) error
	OnResharingResult(callback func(event event.ResharingResultEvent)) error
}

// reshareKeyTypes is the ordered set of key families to reshare per wallet.
// EdDSA first, then ECDSA (auto_reshare_design.md §4.4): both must be updated
// or the ECDSA signing path breaks.
var reshareKeyTypes = []types.KeyType{types.KeyTypeEd25519, types.KeyTypeSecp256k1}

// Publisher publishes reshare events and waits for their results (§4.4/§7).
type Publisher struct {
	client     ReshareClient
	authorizer *AuthorizerClient
	timeout    time.Duration

	mu      sync.Mutex
	waiters map[string]chan event.ResharingResultEvent
}

// NewPublisher creates a publisher over the given mpcium client. When authorizer
// is enabled (§5.3D) every reshare event is signed by all configured authorizers
// before publishing; pass a disabled/nil client for Phase 1 behavior.
func NewPublisher(client ReshareClient, authorizer *AuthorizerClient, resultTimeout time.Duration) *Publisher {
	return &Publisher{
		client:     client,
		authorizer: authorizer,
		timeout:    resultTimeout,
		waiters:    make(map[string]chan event.ResharingResultEvent),
	}
}

// Start subscribes to reshare results and routes them to per-(wallet,keytype)
// waiters. Call once before ReshareWallet.
func (p *Publisher) Start() error {
	return p.client.OnResharingResult(func(e event.ResharingResultEvent) {
		p.deliver(e)
	})
}

func waiterKey(walletID string, kt types.KeyType) string {
	return walletID + "|" + string(kt)
}

func (p *Publisher) deliver(e event.ResharingResultEvent) {
	p.mu.Lock()
	ch := p.waiters[waiterKey(e.WalletID, e.KeyType)]
	p.mu.Unlock()
	if ch != nil {
		select {
		case ch <- e:
		default:
		}
	}
}

func (p *Publisher) register(key string) chan event.ResharingResultEvent {
	ch := make(chan event.ResharingResultEvent, 1)
	p.mu.Lock()
	p.waiters[key] = ch
	p.mu.Unlock()
	return ch
}

func (p *Publisher) unregister(key string) {
	p.mu.Lock()
	delete(p.waiters, key)
	p.mu.Unlock()
}

// AttemptedKeyTypes returns the key families a reshare call will attempt: the
// explicit `only` set, or the full sequential set when empty.
func AttemptedKeyTypes(only []types.KeyType) []types.KeyType {
	if len(only) == 0 {
		return append([]types.KeyType(nil), reshareKeyTypes...)
	}
	return only
}

// RemainingKeyTypes returns the attempted families that did not complete, in
// their canonical reshare order — used to persist §7.1 partial progress.
func RemainingKeyTypes(attempted []types.KeyType, done map[types.KeyType]bool) []types.KeyType {
	var out []types.KeyType
	for _, kt := range attempted {
		if !done[kt] {
			out = append(out, kt)
		}
	}
	return out
}

// keyTypeStrings renders key families for structured logs.
func keyTypeStrings(kts []types.KeyType) []string {
	out := make([]string, len(kts))
	for i, kt := range kts {
		out[i] = string(kt)
	}
	return out
}

// ReshareResult reports which key families were resharded successfully, and the
// resulting public key per family (for §5E/§7.4 post-reshare verification).
type ReshareResult struct {
	Done    map[types.KeyType]bool
	PubKeys map[types.KeyType][]byte
}

// AllDone reports whether every key family completed.
func (r ReshareResult) AllDone() bool {
	for _, kt := range reshareKeyTypes {
		if !r.Done[kt] {
			return false
		}
	}
	return true
}

// ReshareWallet publishes reshare events for the given committee and waits for
// each key family's result sequentially (§4.4). It returns a partial result and
// an error if any family fails or times out; EdDSA failure stops before ECDSA.
func (p *Publisher) ReshareWallet(ctx context.Context, walletID string, committee []string, newThreshold int, only []types.KeyType) (ReshareResult, error) {
	res := ReshareResult{
		Done:    make(map[types.KeyType]bool),
		PubKeys: make(map[types.KeyType][]byte),
	}

	types_ := only
	if len(types_) == 0 {
		types_ = reshareKeyTypes
	}

	for _, kt := range types_ {
		pubKey, err := p.reshareOneWithRetry(ctx, walletID, committee, newThreshold, kt)
		if err != nil {
			// EdDSA failure stops before ECDSA (§4.4 sequential); the partial
			// result (res.Done) lets the caller reshare only what remains (§7.1).
			return res, fmt.Errorf("reshare %s for wallet %s: %w", kt, walletID, err)
		}
		res.Done[kt] = true
		res.PubKeys[kt] = pubKey
	}
	return res, nil
}

// reshareOneWithRetry runs reshareOne up to reshareMaxTries times (§7.1).
func (p *Publisher) reshareOneWithRetry(ctx context.Context, walletID string, committee []string, newThreshold int, kt types.KeyType) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= reshareMaxTries; attempt++ {
		pubKey, err := p.reshareOne(ctx, walletID, committee, newThreshold, kt)
		if err == nil {
			return pubKey, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, lastErr
		}
		if attempt < reshareMaxTries {
			logger.Warn("Reshare key type failed; retrying with same committee (new session)",
				"walletID", walletID, "keyType", string(kt), "attempt", attempt, "error", lastErr.Error())
		}
	}
	return nil, lastErr
}

func (p *Publisher) reshareOne(ctx context.Context, walletID string, committee []string, newThreshold int, kt types.KeyType) ([]byte, error) {
	key := waiterKey(walletID, kt)
	ch := p.register(key)
	defer p.unregister(key)

	msg := &types.ResharingMessage{
		SessionID:    uuid.New().String(),
		NodeIDs:      committee,
		NewThreshold: newThreshold,
		KeyType:      kt,
		WalletID:     walletID,
	}
	if p.authorizer.Enabled() {
		collect := func(authorizerRaw []byte) ([]types.AuthorizerSignature, error) {
			return p.authorizer.Collect(ctx, authorizerRaw)
		}
		if err := p.client.ResharingWithAuthorizers(msg, collect); err != nil {
			return nil, fmt.Errorf("publish (authorized): %w", err)
		}
	} else if err := p.client.Resharing(msg); err != nil {
		return nil, fmt.Errorf("publish: %w", err)
	}

	timer := time.NewTimer(p.timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("timeout after %s waiting for result", p.timeout)
	case e := <-ch:
		if e.ResultType != event.ResultTypeSuccess {
			reason := e.ErrorReason
			if reason == "" {
				reason = e.ErrorCode
			}
			return nil, fmt.Errorf("reshare failed: %s", reason)
		}
		if len(e.PubKey) == 0 {
			return nil, fmt.Errorf("reshare result has empty pubkey")
		}
		return e.PubKey, nil
	}
}

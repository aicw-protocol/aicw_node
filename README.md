# AICW MPC Node

A permissionless MPC node for distributed signing of AICW AI-agent wallets on Solana.

## Overview

AICW MPC Node is a threshold signing participant in the AICW network. Each node receives keygen and signing requests over NATS, joins an MPC ceremony with peer nodes, and contributes its share to produce Ed25519/ECDSA keys and signatures without ever exposing a full private key on a single machine.

Nodes discover peers dynamically via Consul, verify membership against an operator-managed whitelist (Phase A), and exchange session keys over NATS before participating in TSS operations.

## Status

**Work in progress.** Phase A (dynamic join) is under active development and not yet production-ready. **Do not use with real funds.**

## Build

```bash
go build -o aicw-node ./cmd/aicw-node
go build -o operator ./cmd/operator
```

## Documentation

See the inline code comments and `config/*.template` files for configuration options.
Operator and design documentation will be published as the project matures.

## License

New and modified code in this repository is licensed under the [MIT License](LICENSE).

This project is derived from [Mpcium](https://github.com/fystack/mpcium). The
original Apache License 2.0 text is preserved in [LICENSE-MPCIUM](LICENSE-MPCIUM)
and must not be removed. Third-party notices, including tss-lib, are in
[NOTICE](NOTICE).

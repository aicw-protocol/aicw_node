# AICW Node — Operator Manual

> **Status: Work in progress.** AICW Node is under active development and is
> not yet production-ready. **Do not use it with real funds or on a network
> that secures real assets.** This manual describes how to run a node on a
> test network.

This guide walks you through running a single AICW MPC node. It assumes **no
prior experience** with Go or Docker. Where a technical term appears for the
first time, it is explained in one line.

---

## 1. What this is

**AICW Node** is a program you run on a computer (a "node") that helps sign
transactions for AICW AI-agent wallets. Signing is **distributed**: no single
node — and no single person — can produce a signature alone. A wallet's key is
split across several nodes using **threshold cryptography** (a technique where
a minimum number of parties must cooperate to sign). Your node holds only a
*share*, never a full private key.

## 2. What your node does

- Receives key-generation and signing requests over the network.
- Discovers other nodes automatically (you do **not** edit a peer list by hand).
- Exchanges session keys with the other nodes and contributes its share to
  produce a signature.

You run **one** node. The other nodes are found automatically.

---

## 3. Requirements

| Item | Minimum | Recommended |
|------|---------|-------------|
| CPU  | 2 cores | 4 cores or more |
| RAM  | 4 GB    | 8 GB or more |
| Disk | 20 GB   | 50 GB or more |
| OS   | Linux x64, Windows 10+, macOS | Ubuntu 22.04 LTS |
| Network | Stable outbound internet | — |

You will also need, **from the network operator** (the person coordinating the
network you are joining):

- A **network config file** (`network-config.yaml`) with the shared settings
  already filled in.
- **Membership approval** — after you create your identity (Section 5), you
  send them your `node_id` and `public_key`, and they add you to the allow-list.

---

## 4. Choosing how to run your node

There are three ways to run a node. **Docker is not required.** Pick one:

| Path | Difficulty | Use when |
|------|-----------|----------|
| **1. Binary download** | Easiest | You just want to get running. No Go, no Docker. |
| **2. Docker** | Medium | You want to keep the node running long-term with automatic restarts. Requires Docker installed. |
| **3. Build from source** | Advanced | You are a developer and want to build it yourself. |

Sections 6–8 cover each path. Everyone should first do Section 5 (create your
identity).

---

## 5. Create your identity (all paths)

Your **identity** is your node's name plus its cryptographic keys. Creating it
is now a **single command** — no manual steps, no separate tools.

Pick a node name (letters, digits, hyphens — e.g. `alice`). Then run:

```
aicw-node init --name alice
```

This one command:

- generates a unique `node_id`,
- generates the node's key pair,
- writes `identity/alice_identity.json` and `identity/alice_private_key.txt`.

Example output:

```
Identity created successfully.
  node_name:  alice
  node_id:    4e099787-24e8-4746-894d-5c56ddef2a58
  public_key: 17f2b294b17d4c5d7f151353ea4582866ee6f549e61e4507f8b9bde6ee242e0c
Files written:
  identity/alice_identity.json
  identity/alice_private_key.txt
```

Options:

- `--output-dir <dir>` — where to write the files (default: `identity/`).
- `--overwrite` — replace existing identity files.

**Send your `node_id` and `public_key` to the network operator** and ask to be
added to the membership allow-list. You do **not** send your private key.

> **Never share `alice_private_key.txt`.** It is secret. Do not upload it, email
> it, paste it into chat, or commit it to Git. See Section 10.

---

## 6. Path 1 — Binary download (easiest, no Docker)

1. Go to the project's **GitHub Releases** page.
2. Download the file for your operating system (Windows, macOS, or Linux).
3. Put it in a folder together with:
   - the `network-config.yaml` the operator gave you,
   - your `identity/` folder from Section 5,
   - a `password.txt` file containing one strong random line (this encrypts the
     node's local database).
4. Start the node:

   **Linux / macOS**
   ```
   ./aicw-node start --network-config network-config.yaml --name alice --password-file password.txt
   ```

   **Windows**
   ```
   .\aicw-node.exe start --network-config network-config.yaml --name alice --password-file password.txt
   ```

That's it. No Go, no Docker, no building.

---

## 7. Path 2 — Docker (for long-term running)

Docker runs your node inside a self-contained container and can restart it
automatically if it stops or the machine reboots. You need
[Docker](https://www.docker.com/) installed.

1. Clone this repo (the `docker-compose.yaml` lives at its root) and `cd` into it.
2. Do Section 5 (`aicw-node init --name alice`) **inside this folder**; it
   writes to `identity/`, which the compose file mounts into the container.
3. Create a **`config/` subfolder** (note: not the project root) with two
   files:
   ```
   mkdir config
   cp config/network-config.yaml.template config/network-config.yaml
   # edit config/network-config.yaml: fill in the values the operator gave you
   echo "some-strong-random-string" > config/password.txt
   ```
4. Start:
   ```
   docker compose up -d
   ```
   (`-d` runs it in the background.)

   By default this **builds the image from source**, which requires the
   `mpcium` fork to be cloned as a sibling folder next to this repo (see the
   note in Section 8). If you don't have that, pull the pre-built image
   instead once it's published:
   ```
   docker compose pull
   docker compose up -d --no-build
   ```

To change your node's name, don't edit the compose file — set an environment
variable when starting:
```
NODE_NAME=alice docker compose up -d
```
(or put `NODE_NAME=alice` in a `.env` file next to `docker-compose.yaml`).

To keep it running across restarts, the provided compose file uses a restart
policy; leave it as configured.

To view logs (the node logs to stdout only; there is no separate log file
inside the container):
```
docker compose logs -f
```

To stop:
```
docker compose down
```

---

## 8. Path 3 — Build from source (advanced)

For developers who want to compile the node themselves. Requires **Go 1.25+**
and **Git**.

> **This repo depends on a local fork of `mpcium`** (the underlying MPC
> library, patched with AICW's peer-management and DoS-gate changes) via a
> `go.mod` `replace` directive. Cloning only `aicw_node` and running
> `go build` **will fail** with an error like
> `replacement directory ../mpcium does not exist`. You must clone the AICW
> `mpcium` fork (ask the project maintainers for the URL if it isn't public
> yet — this is **not** the same as the upstream `fystack/mpcium` repo) as a
> **sibling folder**, right next to `aicw_node/`:
>
> ```
> some-folder/
>   aicw_node/   <- this repo
>   mpcium/      <- the AICW fork, sibling directory, same parent folder
> ```

```
git clone https://github.com/aicw-protocol/aicw_node.git
git clone <the-aicw-mpcium-fork-url> mpcium   # must sit next to aicw_node/
cd aicw_node
go build -o aicw-node ./cmd/aicw-node
```

Then create your identity (Section 5) and start the node as in Path 1.

---

## 9. Verifying it works

After starting, watch the log output. A healthy node shows, in order:

1. `Loaded self identity` — your `node_id` is recognized.
2. `Registered self identity to Consul` — the node announced itself.
3. `[READY] AICW Node is ready` — the node is up.
4. When other nodes are present:
   `[ECDH] Key exchange complete: N/N symmetric keys established` — secure
   channels with the other nodes are set up.

> The first time a node joins, key exchange can take up to about a minute while
> it finds the other nodes. This is normal — do not assume it has failed if
> signing is not immediately available.

---

## 10. Security checklist

- [ ] **Never share your private key** (`{name}_private_key.txt` or
      `{name}_private.key`). Not in Git, chat, email, or cloud drives.
- [ ] Keep `password.txt` local and private.
- [ ] Store an **offline backup** of your `identity/` folder (e.g. an encrypted
      USB drive), so you can recover your node.
- [ ] Firewall: allow outbound connections to the network's services; you do not
      need to expose inbound ports except a health port if you enable one.
- [ ] Keep the machine's clock accurate (use automatic time sync).

---

## 11. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `identity file not found` | Name mismatch | Ensure `--name` matches your `{name}_identity.json` |
| `failed to read private key file` | Wrong key filename | The node accepts both `{name}_private_key.txt` and `{name}_private.key`; make sure one exists |
| `peer membership verification failed` | Not yet approved | Ask the operator to add your `node_id` + `public_key` to the allow-list |
| `symmetric key not found` right after joining | Key exchange still in progress | Wait up to ~1 minute; nodes re-broadcast periodically |
| Node starts but never reaches `[READY]` | Cannot reach network services | Check the `network-config.yaml` values and your internet connection |
| Keygen/signing fails for everyone | Wrong shared settings | The `chain_code` and initiator key must exactly match the operator's values (they are in `network-config.yaml`) |
| `Badger password is required` (node exits immediately) | No password supplied | Pass `--password-file password.txt` (binary) or check `config/password.txt` is mounted (Docker) |
| `Event initiator public key is required` / `chain_code is required` | Still using placeholder template values | Open `network-config.yaml` and make sure `REPLACE_WITH_...` placeholders were actually replaced with real values from the operator |
| `flag provided but not defined: -xxx` | Typo or outdated flag in your start command | Run `aicw-node start --help` to see the exact flags this version supports |

---

## 12. Stopping and updating

**Stop:** press `Ctrl+C` (binary) or run `docker compose down` (Docker). The
node cleans up its registration on exit.

**Update:** download the new binary (Path 1) or pull the new image (Path 2),
then start again with the same identity and config. Your identity does not
change.

---

## 13. Configuration reference

Two config files keep things simple:

- **`network-config.yaml`** — shared network settings (network endpoints,
  `chain_code`, initiator public key, signing threshold). **The operator gives
  you this;** you do not fill in long hex values by hand.
- **`operator-config.yaml`** *(optional)* — your personal overrides, merged on
  top of the network config.

Templates are provided as `config/network-config.yaml.template` and
`config/operator-config.yaml.template`.

---

*AICW Node is open source under the MIT license. This manual describes a
work-in-progress system; commands and options may change between releases.*

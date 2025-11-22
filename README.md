
<p align="center">
  <img src="./assets/logo-brutal.svg" width="100%">
A Sudoku‑based proxy protocol that ushered in the era of clear‑text / low‑entropy proxies.
</p>

# Sudoku ASCII

[![Build Status](https://img.shields.io/github/actions/workflow/status/Futaiii/Sudoku_ASCII/.github/workflows/release.yml?branch=main&style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/actions)
[![Latest Release](https://img.shields.io/github/v/release/Futaiii/Sudoku_ASCII?style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/releases)
[![License](https://img.shields.io/github/license/Futaiii/Sudoku_ASCII?style=for-the-badge)](./LICENSE)

[中文文档](https://github.com/Futaiii/Sudoku_ASCII/blob/main/README.zh_CN.md)

Sudoku ASCII is a traffic obfuscation protocol based on combinatorial mathematics. It disguises arbitrary data streams as generated 4×4 Sudoku puzzle clues, making encrypted traffic appear as ordinary logic‑game data.

The core idea of the project is to exploit the mathematical properties of Sudoku grids to achieve **O(1)**‑complexity fast encoding and decoding while providing resistance to active probing.

## Core Features

### Sudoku Steganography Algorithm
Unlike traditional random‑noise obfuscation, this protocol uses a pre‑computed Sudoku solution permutation table to convert each byte of data into a set of “clue numbers” on a Sudoku board.

- **Dynamic Padding**: At any moment, any length of non‑data bytes can be inserted at arbitrary positions, hiding protocol fingerprints.
- **Data Hiding**: The distribution of padding bytes closely matches that of plaintext bytes (65 %–100 % ASCII proportion), preventing identification via statistical analysis.
- **Low Information Entropy**: Overall byte Hamming weight is about **3.0** (in low‑entropy mode), lower than the 3.4–4.6 reported by GFW Report.
- **Efficient Mapping**: Uses a space‑for‑time strategy; the mapping table is generated once at initialization, and runtime operations are simple table lookups.

> **Note:** 100 % ASCII proportion applies to the ASCII‑preferred mode; the ENTROPY‑preferred mode yields 65 %. A Hamming weight of 3.0 applies to the ENTROPY‑preferred mode; the ASCII‑preferred mode yields 4.0.  
> Currently there is no evidence that either priority strategy leaves a noticeable fingerprint.

### Security & Encryption
Below the obfuscation layer, the protocol optionally employs AEAD (Authenticated Encryption with Associated Data) to protect integrity and confidentiality.

- **Algorithm Support**: AES‑128‑GCM or ChaCha20‑Poly1305.
- **Replay Protection**: Handshake includes a timestamp check to thwart replay attacks.

### Defensive Fallback
When the server detects an illegal handshake, a timed‑out connection, or malformed packets, it does **not** terminate the connection (a common proxy‑detection sign). Instead, it silently forwards the connection to a designated decoy address (e.g., an Nginx or Apache server). The probe only sees a normal web‑server response.

### Limitations (TODO)

1. **Packet Format**: Supports TCP packets only.  
2. **Bandwidth Utilization**: Below 30 %; best for users with high‑speed or high‑bandwidth connections, or for “airport” operators to increase traffic.  
3. **Client Proxy**: SOCKS5 only.  
4. **Protocol Adoption**: No Android/GUI clients yet, and limited kernel compatibility.



## Quick Start

### Build

```bash
go build -o sudoku cmd/sudoku-tunnel/main.go
```

### Server Configuration (`config.json`)

```json
{
  "mode": "server",
  "local_port": 1080,
  "server_address": "",
  "fallback_address": "127.0.0.1:80",
  "key": "your-secret-key",
  "aead": "chacha20-poly1305",
  "suspicious_action": "fallback",
  "ascii": "prefer_entropy",
  "padding_min": 2,
  "padding_max": 7
}
```

### Client Configuration

Change `"mode"` to `"client"`, set `"server_address"` to the server’s IP, set `"local_port"` to the proxy listening port, and add `"rule_urls"` using the template from `configs/config.json`.

### Run

Set the path to `config.json` as the argument for `-c`. 

```bash
./sudoku -c config.json
```

## Protocol Flow

1. **Initialization**: Client and server generate identical Sudoku mapping tables from the pre‑shared key.
2. **Handshake**: Client sends an encrypted timestamp and random nonce.
3. **Transmission**: Data → AEAD encrypt → slice → map to Sudoku clues → add padding → send.
4. **Reception**: Receive data → filter padding → restore Sudoku clues → table lookup decode → AEAD decrypt.

---

## Disclaimer
> [!NOTE]\
> This software is for educational and research purposes only. Users must comply with local network regulations.

## Acknowledgments

- [Link 1](https://gfw.report/publications/usenixsecurity23/zh/)
- [Link 2](https://github.com/enfein/mieru/issues/8)
- [Link 3](https://github.com/zhaohuabing/lightsocks)
- [Link 4](https://imciel.com/2020/08/27/create-custom-tunnel/)
- [Link 5](https://oeis.org/A109252)
- [Link 6](https://pi.math.cornell.edu/~mec/Summer2009/Mahmood/Four.html)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Futaiii/Sudoku_ASCII&type=Date)](https://star-history.com/#Futaiii/Sudoku_ASCII)

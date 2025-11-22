
<p align="center">
  <img src="./assets/logo-brutal.svg" width="100%">
    A Sudoku-based proxy protocol, ushering in the era of plaintext / low-entropy proxies.
</p>

# Sudoku ASCII

[![Build Status](https://img.shields.io/github/actions/workflow/status/Futaiii/Sudoku_ASCII/.github/workflows/release.yml?branch=main&style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/actions)
[![Latest Release](https://img.shields.io/github/v/release/Futaiii/Sudoku_ASCII?style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

[中文文档](https://github.com/Futaiii/Sudoku_ASCII/blob/main/README.zh_CN.md)


**SUDOKU** is a traffic obfuscation protocol based on 4x4 Sudoku puzzle setting and solving. It maps arbitrary data streams (where data bytes have at most 256 possibilities, while 4x4 Sudoku has 288 non-isomorphic forms) into uniquely solvable Sudoku puzzles using 4 clues. Since each puzzle has at least one setting scheme, the randomization process ensures that the same data can be encoded into multiple combinations, creating obfuscation.

The core concept of this project is to leverage the mathematical properties of Sudoku grids to encode and decode byte streams, while providing arbitrary padding and resistance against active probing.

## Core Features

### Sudoku Steganography Algorithm
Unlike traditional random noise obfuscation, this protocol uses various masking schemes to map data streams into complete ASCII printable characters. To packet capture tools, it looks like plain text. Alternatively, other masking schemes can be used to ensure the data stream has sufficiently low entropy.

*   **Dynamic Padding**: Insert arbitrary length non-data bytes at any position at any time to hide protocol characteristics.
*   **Data Hiding**: The distribution characteristics of padding bytes match those of the plaintext bytes (65%~100%* ASCII ratio), avoiding identification via data distribution analysis.
*   **Low Information Entropy**: The overall byte Hamming weight is around 3.0* (in low entropy mode), which is lower than the 3.4~4.6 range typically blocked by firewalls as mentioned in GFW Reports.

---

> *Note: A 100% ASCII ratio requires `ASCII preferred` mode; `ENTROPY preferred` mode yields 65%. A Hamming weight of 3.0 requires `ENTROPY preferred` mode; `ASCII preferred` mode yields 4.0.

> Currently, there is no evidence indicating that either preference strategy has a distinct fingerprint.

---

### Uplink/Downlink Separation
#### — An Attempt to Resolve Downstream Bandwidth Issues Based on the API Provided by [mieru](https://github.com/enfein/mieru/tree/main)
> Special thanks to the developer of mieru

Since the stream encapsulation of the sudoku protocol leads to increased packet size, bandwidth limitations may occur in streaming and downloading scenarios (theoretically, with 200 Mbps symmetrical uplink/downlink on both the local side and the VPS, bottlenecks should not exist). Therefore, the mieru protocol, which is also a non-TLS solution, is adopted as an (optional) downstream protocol.

#### mieru Configuration
```json
  "enable_mieru": true,
  "mieru_config": {
    "port": 20123,
    "transport": "TCP",
    "mtu": 1400,
    "multiplexing": "HIGH",
    "username": "sudoku_user",
    "password": "your_secure_password_here"
  }
```
**Explanation:** When `"enable_mieru"` is set to `true`, uplink/downlink separation is enabled; when set to `false`, the `"mieru_config"` field can be ignored. Within the `"mieru_config"` field, `port` is mandatory (specifying the downstream port), while other configuration items can be omitted.

### Security & Encryption
Beneath the obfuscation layer, the protocol optionally uses AEAD to protect data integrity and confidentiality.
*   **Algorithm Support**: AES-128-GCM or ChaCha20-Poly1305.
*   **Anti-Replay**: The handshake phase includes timestamp validation to effectively prevent replay attacks.

### Defensive Fallback
When the server detects illegal handshake requests, connection timeouts, or malformed packets, it does not disconnect immediately . Instead, it seamlessly forwards the connection to a designated decoy address (such as an Nginx or Apache server). Probers will only see a standard web server response.

### Drawbacks (TODO)
1.  **Packet Format**: Only supports TCP packets.
2.  **Bandwidth Utilization**: Less than 30%. Recommended for users with high bandwidth or good network lines. Also recommended for VPN service providers, as it effectively increases user traffic consumption.
3.  **Client Proxy**: Only supports SOCKS5/HTTP.
4.  **Protocol Adoption**: No Android/GUI support or other kernel compatibility yet. (A workaround exists: whitelist your VPS IP in rules like Clash, run this protocol locally, then add a SOCKS proxy in your proxy client pointing to this protocol's port).

## Quick Start

### Build

```bash
go build -o sudoku cmd/sudoku-tunnel/main.go
```

### Server Configuration (config.json)

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

Change `mode` to `client`, set `server_address` to the server IP, set `local_port` to the proxy listening port, and add `rule_urls`. You can use the template in `configs/config.json` to fill it out.

### Run
Run the program specifying the path to `config.json` as an argument.
```bash
./sudoku -c config.json
```

## Protocol Flow

1.  **Initialization**: Client and Server generate the same Sudoku mapping table based on the Pre-Shared Key (Key).
2.  **Handshake**: Client sends encrypted timestamp and random nonce.
3.  **Transmission**: Data -> AEAD Encryption -> Slicing -> Map to Sudoku Clues -> Add Padding -> Send.
4.  **Reception**: Receive Data -> Filter Padding -> Restore Sudoku Clues -> Lookup Table Decoding -> AEAD Decryption.

---

## Disclaimer
> [!NOTE]\
> This software is for educational and research purposes only. Users are responsible for complying with local network regulations.

## Acknowledgments

- [Link 1](https://gfw.report/publications/usenixsecurity23/zh/)
- [Link 2](https://github.com/enfein/mieru/issues/8)
- [Link 3](https://github.com/zhaohuabing/lightsocks)
- [Link 4](https://imciel.com/2020/08/27/create-custom-tunnel/)
- [Link 5](https://oeis.org/A109252)
- [Link 6](https://pi.math.cornell.edu/~mec/Summer2009/Mahmood/Four.html)


## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Futaiii/Sudoku_ASCII&type=Date)](https://star-history.com/#Futaiii/Sudoku_ASCII)

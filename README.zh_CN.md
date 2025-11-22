<p align="center">
  <img src="./assets/logo-cute.svg" width="100%">
    一种基于数独的代理协议，开启了明文 / 低熵代理时代
</p>

# Sudoku ASCII

[![构建状态](https://img.shields.io/github/actions/workflow/status/Futaiii/Sudoku_ASCII/.github/workflows/release.yml?branch=main&style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/actions)
[![最新版本](https://img.shields.io/github/v/release/Futaiii/Sudoku_ASCII?style=for-the-badge)](https://github.com/Futaiii/Sudoku_ASCII/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

Sudoku ASCII 是一个基于组合数学的流量混淆协议。它通过将任意数据流映射为生成的 4x4 数独谜题提示，将加密流量伪装成普通的逻辑游戏数据。

该项目的核心理念是利用数独网格的数学特性，实现 O(1) 复杂度的快速编码与解码，同时提供抗主动探测能力。

## 核心特性

### 数独隐写算法
不同于传统的随机噪音混淆，本协议通过预计算的数独终盘置换表，将每一个字节的数据转换为数独盘面上的一组"提示数"。
*   **动态填充**: 在任意时刻任意位置填充任意长度非数据字节，隐藏协议特征。
*   **数据隐藏**: 填充字节的分布特征与明文字节分布特征基本一致(65%~100%*的ASCII占比)，可避免通过数据分布特征识别明文。
*   **低信息熵**: 整体字节汉明重量约在3.0*（低熵模式下）,低于GFW Report提到的3.4~4.6。
*   **高效转换**: 利用空间换时间策略，初始化时生成映射表，运行时仅需查表操作。

---

> *注：100%的ASCII占比须在ASCII优先模式下，ENTROPY优先模式下为65%。 3.0的汉明重量须在ENTROPY优先模式下，ASCII优先模式下为4.0.

> 目前没有任何证据表明两种优先策略的任何一种有明显指纹。

---

### 安全与加密
在混淆层之下，协议可选的采用 AEAD（关联数据的认证加密）保护数据完整性与机密性。
*   **算法支持**: AES-128-GCM 或 ChaCha20-Poly1305。
*   **防重放**: 握手阶段包含时间戳校验，有效防止重放攻击。

### 防御性回落 (Fallback)
当服务器检测到非法的握手请求、超时的连接或格式错误的数据包时，不会直接断开连接（这通常是识别代理服务器的特征），而是将连接无缝转发至指定的诱饵地址（如 Nginx 或 Apache 服务器）。探测者只会看到一个普通的网页服务器响应。

### 缺点（TODO）
1.  **数据包格式**: 仅支持 TCP 数据包。
2.  **带宽利用率**: 低于30%，推荐线路好的或者带宽高的用户使用，另外推荐机场主使用，可以有效增加用户的流量。
3.  **客户端代理**: 仅支持socks5。
4.  **协议普及度**: 暂无安卓/图形化，以及其他内核兼容。



## 快速开始

### 编译

```bash
go build -o sudoku cmd/sudoku-tunnel/main.go
```

### 服务端配置 (config.json)

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

### 客户端配置

将 `mode` 改为 `client`，并设置 `server_address` 为服务端 IP，将`local_port` 设置为代理监听端口，添加 `rule_urls` 使用`configs/config.json`的模板填充即可。

### 运行
指定 `config.json` 路径为参数运行程序
```bash
./sudoku -c config.json
```

## 协议流程

1.  **初始化**: 客户端与服务端根据预共享密钥（Key）生成相同的数独映射表。
2.  **握手**: 客户端发送加密的时间戳与随机数。
3.  **传输**: 数据 -> AEAD 加密 -> 切片 -> 映射为数独提示 -> 添加填充 -> 发送。
4.  **接收**: 接收数据 -> 过滤填充 -> 还原数独提示 -> 查表解码 -> AEAD 解密。

---


## 声明
> [!NOTE]\
> 此软件仅用于教育和研究目的。用户需自行遵守当地网络法规。

## 鸣谢

- [链接1](https://gfw.report/publications/usenixsecurity23/zh/)
- [链接2](https://github.com/enfein/mieru/issues/8)
- [链接3](https://github.com/zhaohuabing/lightsocks)
- [链接4](https://imciel.com/2020/08/27/create-custom-tunnel/)
- [链接5](https://oeis.org/A109252)
- [链接6](https://pi.math.cornell.edu/~mec/Summer2009/Mahmood/Four.html)


## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Futaiii/Sudoku_ASCII&type=Date)](https://star-history.com/#Futaiii/Sudoku_ASCII)

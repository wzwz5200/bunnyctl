# 协议笔记 · TANCHJIM BUNNY DSP（KT0211LC02）

> 实测设备：`31b2:1112`，固件 `1.0.1`（2024-09-04 构建），序列号 `202409092017`
> 平台：KTMicro（昆腾微）Helios 系 USB Audio DSP，与 KT02H20 / KT0211L 同一寄存器布局

这是 [usagi](../README.md) 的协议底稿：本文所有结论都来自在真机上反复读写的实测，
以及厂商 Windows 工具（KT_USB_APP / KT_BOOT_TOOL）的静态分析。

**为什么会有这份文档**：BUNNY DSP 的调音依赖官方 App，而官方只提供
Android / iOS / Windows 客户端（[下载中心](https://tanchjim.com/services/)），
没有 Linux 版，也没有任何公开协议。下面的报文格式和寄存器地图是黑盒试出来的，
供移植、备份和排障参考。

> ⚠️ **写 flash 有风险**：`0x53` 会把当前 RAM 状态固化进耳机 flash。
> 动手之前先用 `python3 tools/ktdump.py -o flash.bin` 备份整片固件，写错了还能烧回去。

---

## 1. 设备档案

| 项 | 值 | 来源 |
|---|---|---|
| VID:PID | `31b2:1112` | 描述符 / 寄存器 `0x5B` |
| 厂商串 | `KTMicro` | `0x40` |
| 产品串 | `TANCHJIM BUNNY DSP` | `0x48` |
| 序列号 | `202409092017` | `0x50` |
| 固件 | `1.0.1`，`Sep  4 2024` | `0x04` / `0x08` |
| flags | `0x00000001`（bit9=0 → 立体声 DAC） | `0x01` |
| 魔数 | `0x12345678` ✔ | `0xE1` |

USB 拓扑（UAC 1.0，全速 12 Mbps）：

- IF0 AudioControl：Mic → FU(2) → SU(3) → OT(4)；USB Streaming → MU(6) → FU(7) → Speaker(8)
- IF1 AudioStreaming IN：48 kHz 单声道 16 bit（麦克风上行）
- IF2 AudioStreaming OUT：44.1/48/96 kHz，16 / 24 bit 立体声（耳机下行）
- **IF3 HID**：中断 IN/OUT 各 16 字节，厂商控制通道 ← 所有 DSP 操作都走这里

## 2. HID 报文

HID 报告描述符里定义了两组厂商页（`0xFF01`）报告：报告 ID `0x4B` 与 `0x54`，各带 10 字节 IN + 10 字节 OUT。`0x4B` = 寄存器通道，`0x54` = 模式/引导通道。

帧格式（固定 11 字节 = 1 字节报告 ID + 10 字节负载）：

```
读寄存器  TX  4B <addr LE32> 52 00 00 00 00 00
          RX  4B <addr LE32> 52 00 <value LE32>
写寄存器  TX  4B <addr LE32> 57 00 <value LE32>
          RX  4B <addr LE32> 57 00 <status LE32>      status & 0xFF = 0x03
存盘      TX  4B 00 00 00 00 53 00 00 00 00 00
          RX  4B 00 00 00 00 53 00 03 00 00 00       成功后设备重启
扩展读    TX  4B <addr LE32> 08 00 00 00 00 00
          RX  4B <value LE32> 00 ...                  value 在 payload[0..3]
```

注意点（都是实测踩出来的）：

- 握手 `0x43` **不要发**：在这颗芯片上会 stall HID 管道甚至让设备重枚举。
- 读寄存器**不需要**任何握手，直接 `0x52` 即可。
- `0x08` 扩展读**只能 4 字节对齐**。发 `0x106A` 这种非对齐地址会让固件卡死，HID 端点超时，整机从总线掉落，只能拔插恢复。

## 3. 寄存器地图（本机实测值）

```
0x00  3A474552  "REG:"          字符串区起始标记
0x01  00000001  flags           bit9 = 单 DAC 机型
0x04  "1.0.1"                   版本串（LE32 ASCII，每 4 字节一个字）
0x08  "Sep  4 2024"             构建日期
0x0C  "11:02:40"                构建时间
0x10  "a0fc753"                 构建哈希
0x18  00000000  ADC EQ 使能     bit0 = ON（麦克风路径，出厂关闭）
0x1A~0x23       ADC EQ 5 段     1000/2000/5000/8000/10000 Hz，0 dB，Q0.707
0x24  00000001  DAC EQ 使能     bit0 = ON（耳机路径，出厂开启）
0x26~0x2F       DAC EQ 5 段     见第 4 节
0x36  "Perf"    0x37 "Cfg:"     性能配置标记
0x3A  00000005  A_ADC PGA       索引 → {0,-6,8,14,20,26,32,44} dB
0x3B  0000000B  A_DAC PGA       0=mute；i=1..15 → 1.5(i-1)-18 dB（本机 -3 dB）
0x40  "KTMicro"                 厂商标识串
0x48  "TANCHJIM BUNNY DSP"
0x50  "202409092017"
0x5B  111231B2                  [PID:16][VID:16]
0x61  "1***"    0x64 1          产品密钥/配置字
0x65  00000002  DIG_ADC         有符号字节，0.5 dB/步（本机 +1.0 dB）
0x66  00000000  DIG_DAC         byte0=L, byte1=R
0x71  3CE2DAD8  NoiseGate       未启用；门限 -40 dB / -38 dB，门音量 -30 dB
0x72  0014000A                  AT 10 ms / RT 20 ms
0x73  01F4000A                  hold 10 ms / 500 ms
0x78  000000FD  Limiter         未启用；阈值 -3 dB
0x79  012C000A                  AT 10 ms / RT 300 ms
0xE1  12345678  magic
```

其余地址（`0x02/0x03/0x11~0x17/0x19/0x25/0x30~0x35/0x38/0x39/0x3C~0x3F/0x5C~0x60/0x62/0x63/0x67~0x70/0x74~0x77/0x7A~0xE0/0xE2~0xFF`）读出来是 0。

## 4. EQ 编码与出厂调音

每段 2 个寄存器：

```
A = [freq_Hz : 16][gain×10 : 16 有符号]      ← 地址 base + 2i
B = [type : 3][保留 : 13][Q×1000 : 16]        ← 地址 base + 2i + 1
type: 0=Peak 1=LPF 2=HPF 3=LowShelf 4=HighShelf
```

本机 DAC 出厂 5 段（这就是这条 DSP 线的“默认调音”）：

| # | 频率 | 增益 | Q | 类型 | 原始寄存器 |
|---|---|---|---|---|---|
| 0 | 30 Hz | −4.0 dB | 0.300 | Peak | `0x001EFFD8` / `0x0000012C` |
| 1 | 200 Hz | −6.0 dB | 0.500 | Peak | `0x00C8FFC4` / `0x000001F4` |
| 2 | 800 Hz | −0.3 dB | 0.500 | Peak | `0x0320FFFD` / `0x000001F4` |
| 3 | 1700 Hz | −4.0 dB | 0.600 | Peak | `0x06A4FFD8` / `0x00000258` |
| 4 | 7500 Hz | −6.0 dB | 0.900 | Peak | `0x1D4CFFC4` / `0x00000384` |

即对“低频隆起 + 7.5 kHz 齿音峰”做补偿的修正曲线，整体只削不增，峰值 −7.1 dB。用 `tools/ktdsp.py` 读出的 biquad 级联响应见 `dac-stock.png`。

## 5. 持久化 —— 实测可用

`0x53` 写入后设备**确实重启**（USB 重新枚举），且重启后寄存器值仍然是刚写进去的值：

```
写 0x26 = +2.0 dB → 0x53 → 设备重启 → 读回 0x26 = 0x001E0014 (+2.0 dB) ✔
再写回 −4.0 dB    → 0x53 → 设备重启 → 读回 0x26 = 0x001EFFD8 ✔
```

也就是说：**不需要刷固件就能把自定义 EQ 固化进 flash**。社区文档里“只能靠改固件镜像”的说法在 Tanchjim 这颗固件上不成立。

## 6. 固件读取 —— 已完整 dump

实际跑完的结果（`tools/ktdump.py`，256 KiB 全片，0 次失败，约 130 秒）：

```
flash.bin  262144 字节
0x00000  KT_lnv1b_flash__0211LC02  ...  ← 芯片型号 KT0211LC02（KT0211L C02）
0x001F0  KT_lnv1b_boot_v1 ... V:1.2 2023-04-27   ← 引导器镜像头
0x00380  代码 + 字符串（含 "FLASH ID ERROR : 0x%08x"）
0x08000  KT_lnv1b_flash_10211LC02  Size=0xA510   ← 应用镜像，2021-10-22
0x09000  配置块 A：256 个寄存器的完整镜像（0x400 字节）
0x0A000  配置块 B：与 A 完全相同（双备份，防掉电写坏）
0x0B000  DSP 参数/微码区，到 0x1250F 结束
0xBFFFF 之后全是 0xFF（空）
```

配置块就是 `0x53` 写入的内容：文件中偏移 `0x9000 + reg*4` 对应运行时的寄存器 `reg`。
校验：镜像里 `reg 0x26 = 0x001EFFD8`、`reg 0x24 = 1`、`reg 0x48` 起是 `"TANCHJIM BUNNY DSP"`，与实时读值逐字节一致。

顺带发现的矛盾点：应用镜像头写的是 2021-10-22，而配置块里的固件串是 `1.0.1 / Sep  4 2024` ——
说明厂家 2024 年重新打包调音时没有更新镜像头里的日期字段，运行时报的才是真的。

因此有两条固化路径：

1. **软路径（推荐）**：运行时改寄存器 → `0x53` 存盘 → 设备重启，双配置块自动同步。
2. **镜像路径**：改 `flash.bin` 的 `0x9098/0xA098` 等位置 → 进 CDC 引导器烧回。
   用于批量/救砖，但风险高（引导器没有读回命令，烧错只能 SWD 救）。

## 6b. 固件读取（原始发现过程）

- `0x08` 扩展读在 `0x80000` 处返回 `"KT_l..."` —— 正好是 KT 镜像头 `KT_lnv1b_flash_1` 的开头，说明**这颗固件的 0x08 直接线性映射内部 flash**（0x80000 = flash 偏移 0，容量 256 KiB / 2 Mbit）。公开资料里“0x08 只能读到 0、不能 dump flash”的结论不适用于本机固件。
- 代价：非 4 字节对齐的地址会把固件读崩（见第 2 节）。所以 dump 必须严格 4 字节步进，脚本 `tools/ktdump.py` 已按此实现并带断点续传。
- 引导模式：HID 报告 `0x54` + `"12345678\0"` 会让设备重枚举成 CDC 串口，之后是 `KTM/VER/KEY/CHP/CFG/PWO/KSTA` + 512 字节 `0x69` 块的烧写协议；公开资料一致确认**烧写端没有读命令**，dump 只能靠上面的 run-mode `0x08` 或硬件 SWD（GPIO4=CLK / GPIO5=DAT）。

## 7. 工具

`tools/ktdsp.py`（纯标准库，走 hidraw，不需要 hidapi/numpy）：

```bash
./tools/ktdsp.py info                       # 身份 + 固件版本
./tools/ktdsp.py dump -o regdump.json       # 256 个寄存器全量导出
./tools/ktdsp.py eq                         # 读 DAC EQ
./tools/ktdsp.py eq --bank adc              # 读 ADC(麦克风) EQ
./tools/ktdsp.py eq --set '0:30,4.0,0.7,PK' --set '1:200,-3,0.5,PK'
./tools/ktdsp.py eq --disable               # 关掉整个 EQ 链路
./tools/ktdsp.py vol                        # PGA + 数字增益
./tools/ktdsp.py drc                        # 噪声门 / 限幅器
./tools/ktdsp.py raw 0x24                   # 原始寄存器读
./tools/ktdsp.py raw 0x24 0                 # 原始寄存器写
./tools/ktdsp.py save --yes                 # 0x53 固化到 flash（设备会重启）
./tools/ktdsp.py eq --plot dac.svg          # 画当前 EQ 的幅频响应
```

`usagi`（本仓库，Web 调音台）：

```bash
make run   # http://127.0.0.1:8099
```

`tools/ktdump.py`（固件 dump）：

```bash
./tools/ktdump.py --check                   # 先探几个对齐地址
./tools/ktdump.py -o flash.bin              # 全片 256 KiB，可断点续传
```

访问权限：已写入 `/etc/udev/rules.d/99-usagi.rules`，给 `31b2:*` 的 hidraw 加了 `plugdev` 组权限和 `/dev/usagi` 软链接，普通用户可直连。

## 8. 安全边界

- 改 EQ / 音量 / DRC / USB 字符串都是 RAM 操作，拔插即恢复出厂；`0x53` 才会写 flash。
- 写 `0x5B`（VID:PID）会让设备以新 ID 重枚举，写错会“消失”；有 udev 规则时可救，但没把握别动。
- `0x54` + `"12345678"` 进 CDC 后，**没有镜像就不要乱烧**：引导器没有读回命令，烧错只能靠 SWD 救。
- `0x08` 非对齐地址 = 死机，必须物理拔插。

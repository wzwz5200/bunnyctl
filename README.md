# bunny-dsp

给 KTMicro USB 音频 DSP 耳机线（TANCHJIM BUNNY DSP，`31b2:1112` / KT0211L）做的 **Web 调音台**：
浏览器里拖曲线，实时写进耳机，一键固化到耳机 flash —— **换台电脑插上，还是这套声音**。

界面按 Material Design 3 深色主题实现（teal 种子色、segmented button、filled text field、
MD3 switch/slider、state layer + ripple、snackbar）。

![界面](docs/ui.png)

## 特性

- **5 段参数均衡**：耳机（DAC）与麦克风（ADC）两条链路，Peak / 低搁架 / 高搁架 / 低通 / 高通
- **实时试听**：拖圆点改频率/增益，滚轮调 Q，松手 ~160ms 自动下发到耳机
- **预设**：平直 / 原厂（从固件读出的出厂调音）/ 低频增强 / 人声 / 耐听 / 哈曼近似
- **AutoEq 导入**：粘贴 `Filter 1: ON PK Fc 105 Hz Gain 5.5 dB Q 0.70` 文本直接套用，Preamp 一起写
- **导出 / 导入 JSON**：曲线可备份、可分享
- **数字前级**：DAC 数字增益 0.5 dB 步进，补回 EQ 削掉的音量
- **写进耳机**：`0x53` 把 RAM 状态提交到 flash，掉电、换电脑都不丢；设备会自动重启
- **恢复出厂**：用固件里 dump 出来的参数覆盖并写回
- 单二进制 + 内嵌前端，无运行时依赖；纯标准库 Go，无 cgo

## 快速开始

需要：**Linux** + **Go 1.22+** + 一条能用的 USB 数据线。

```bash
git clone <this-repo> bunny-dsp
cd bunny-dsp
./install.sh
```

安装脚本会：编译 → 装到 `~/.local/bin/bunny-dsp` → 装 udev 规则（需要 sudo 密码）→
装并启动 systemd 用户服务，最后打印地址。**浏览器不会自动弹出**，自己打开
<http://127.0.0.1:8099> 即可；插拔一次耳机、刷新页面就能用。

```bash
systemctl --user status bunny-dsp    # 运行状态
journalctl --user -u bunny-dsp -f    # 日志
./uninstall.sh                       # 卸载
```

### 只想临时跑一下（不装进系统）

```bash
make run     # 编译并在前台启动，自己访问打印出来的地址
```

首次运行若报无权限读 `/dev/hidraw*`，手动装一次 udev 规则：

```bash
sudo install -m 0644 99-ktmicro-dsp.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger --subsystem-match=hidraw
```

### 手动构建

```bash
make build                              # 产出 ./bunny-dsp
./bunny-dsp                             # 默认 127.0.0.1:8099
./bunny-dsp -addr 0.0.0.0:8099          # 局域网可访问
./bunny-dsp -version
```

## 平台支持

| 平台 | 状态 |
|---|---|
| Linux | ✅ 完整支持（走 `/dev/hidraw` + `select(2)`） |
| Windows / macOS | 🚧 能编译、能打开界面，但设备层会提示“当前系统不支持” |

传输层已经拆成 `hid_linux.go` / `hid_other.go` 两个文件，移植只需要实现
`openHID / Read / Write / hidPaths` 四个函数（Windows 用 SetupAPI + `HidD_*`，macOS 用 IOHIDManager）。

## 怎么用

1. 插上耳机，打开页面，右上角变绿表示连上了
2. 左边拖圆点调曲线，或者点一个预设；改完自动下发，立刻能听到
3. 满意后点 **存到耳机** —— 写进耳机 flash，设备重启 2–3 秒后自动重连
4. 拔下来插到手机 / 播放器 / 别的电脑，就是调好的声音
5. 想还原：点 **恢复出厂**

> 「存到耳机」才写 flash，其它操作都在 RAM 里，拔插即恢复。

## 为什么换台电脑还有效

调音存在耳机自己的 flash（配置块双备份在 `0x9000` / `0xA000`，各 1 KiB），
耳机上电自己加载，跟主机、驱动、播放软件无关。这台电脑上的服务只是“遥控器”。

## 故障排查

| 现象 | 处理 |
|---|---|
| 页面显示未连接，`/dev/hidraw*` 权限不足 | 装 udev 规则（见上），或 `sudo usermod -aG plugdev $USER` 后重新登录 |
| 找不到设备 | `lsusb \| grep 31b2` 确认插入；换条数据线（很多线只有供电） |
| 设备卡死不响应 | 拔插一次；`0x08` 读到非 4 字节对齐地址会导致固件挂起（本工具不会这么做） |
| 端口被占用 | `./bunny-dsp -addr 127.0.0.1:8100` |
| 保存后页面短暂断开 | 正常，`0x53` 会让设备重启，服务会自动重连 |

## 目录结构

```
.
├── main.go              HTTP 服务、路由、flag
├── device.go            寄存器协议、EQ 编解码、保存/重连
├── hid_linux.go         hidraw + select 传输层
├── hid_other.go         非 Linux 的桩实现（保证可编译）
├── presets.go           内置预设 + 出厂参数（从固件 dump）
├── device_test.go       编解码 / 预设 / 寄存器映射测试
├── web/                 前端（嵌入二进制）
│   ├── index.html
│   ├── style.css        MD3 token 与组件
│   └── app.js           曲线绘制、交互、AutoEq 解析
├── install.sh           安装器
├── uninstall.sh         卸载
├── 99-ktmicro-dsp.rules udev 规则
└── docs/ui.png          截图
```

## HTTP API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 存活 + 版本 + 连接状态 |
| GET | `/api/status` | 设备信息、前级、两条 EQ 链 |
| GET | `/api/eq/{dac,adc}` | 读一条 EQ 链 |
| PUT | `/api/eq/{dac,adc}` | 写一条 EQ 链 `{enabled, bands:[{freq,gain,q,type}]}` |
| GET/PUT | `/api/preamp` | 数字前级 `{l,r}`（dB） |
| POST | `/api/save` | `0x53` 写 flash，接口等到设备重启回来再返回 |
| POST | `/api/factory` | 恢复出厂并写 flash |
| GET | `/api/registers` | 256 个寄存器 dump |
| PUT | `/api/raw` | 直接写寄存器 `{addr:"0x24", value:1}` |
| GET | `/api/presets` | 内置预设 |

```bash
curl -s localhost:8099/api/eq/dac
curl -s -X PUT localhost:8099/api/eq/dac -H 'Content-Type: application/json' \
  -d '{"enabled":true,"bands":[{"freq":105,"gain":5.5,"q":0.7,"type":"PK"},
       {"freq":1000,"gain":0,"q":0.7,"type":"PK"},
       {"freq":3000,"gain":-1.5,"q":1,"type":"PK"},
       {"freq":6000,"gain":-2,"q":1.5,"type":"PK"},
       {"freq":10000,"gain":1.5,"q":0.7,"type":"PK"}]}'
curl -s -X POST localhost:8099/api/save
```

## 开发

```bash
make test     # 单元测试
make vet      # go vet
make fmt      # gofmt
```

端到端回归（需要 Firefox，headless 跑真实页面）：

```bash
firefox --headless --marionette --profile /tmp/ffp about:blank &
./bunny-dsp &
python3 test_ui.py        # 点预设 → 校验设备 → 保存 → 恢复出厂
```

协议细节（报文、寄存器地图、固件布局）见 [`docs/PROTOCOL.md`](docs/PROTOCOL.md)。

## 兼容性

在 TANCHJIM BUNNY DSP（`31b2:1112`，固件 1.0.1，KT0211LC02）上完整验证过。
同属 KTMicro Helios 系（KT02H20 / KT0211L）的其它线材大概率可用，但**不同型号的寄存器布局可能不同**，
建议先用 `GET /api/registers` 看一眼再写。

## License

[MIT](LICENSE)

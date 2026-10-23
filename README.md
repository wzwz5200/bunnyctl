# bunnyctl

在 Linux 上给 TANCHJIM BUNNY DSP 调音的 Web 控制台。

![界面](docs/ui.png)

## 背景

TANCHJIM（天使吉米）的 BUNNY（兔几）有一条 USB-C「DSP 版」，调音靠官方 App。

官方客户端只覆盖 Android、iOS 和 Windows（[下载中心](https://tanchjim.com/services/) 里是
`TANCHJIM APP` 和 `TANCHJIM PC`），**没有 Linux 版，也没有公开的协议或驱动**。
于是 Linux 用户要么借一台安卓手机调音，要么就只能听出厂曲线。

这个项目补的就是这块：把这条线的 HID 控制通道逆向出来（[协议笔记](docs/PROTOCOL.md)），
做成一个本地 Web 控制台，浏览器打开就能调，调完写进耳机。

## 安装

需要 **Linux** + **Go 1.22+**。

```bash
git clone https://github.com/wzwz5200/bunnyctl-.git
cd bunnyctl-
./install.sh
```

装好后浏览器打开 <http://127.0.0.1:8099>。脚本会编译、装 udev 规则、注册
systemd 用户服务（`systemctl --user status bunnyctl`），卸载用 `./uninstall.sh`。

只想临时跑：`make run`。

## 功能

- **5 段参数均衡**，耳机（DAC）和麦克风（ADC）两条链，拖圆点实时生效
- **预设**：平直 / 原厂 / 低频增强 / 人声 / 耐听 / 哈曼近似
- **AutoEq 导入**：粘贴 `Filter 1: ON PK Fc ...` 文本直接套用，Preamp 一起写
- **导入导出**：曲线存成 JSON，备份或分享
- **数字前级**：补回 EQ 削掉的音量
- **存到耳机**：写进耳机 flash，换电脑、换手机插上还是这套调音
- **恢复出厂**：用固件里 dump 出来的原厂参数写回

## 用法

1. 插上耳机，打开页面，右上角变绿
2. 拖曲线或点预设，改完自动下发
3. 满意后点「存到耳机」固化

只有「存到耳机」会写 flash，其它操作拔插即恢复。

## 兼容性

| 设备 | 状态 |
|---|---|
| TANCHJIM BUNNY DSP（`31b2:1112` / KT0211LC02 / 固件 1.0.1） | ✅ 已验证 |
| 其它 KTMicro Helios 系（KT02H20 / KT0211L） | ⚠️ 可能可用，寄存器布局未必一致 |

Linux ✅；Windows / macOS 能编译、能开界面，但设备层会提示不支持。

## 故障排查

| 现象 | 处理 |
|---|---|
| 未连接 | 装 `99-bunnyctl.rules`，或 `sudo usermod -aG plugdev $USER` 后重新登录 |
| 找不到设备 | `lsusb \| grep 31b2`，换一条数据线（有的线只供电） |
| 设备卡死 | 拔插一次 |
| 端口占用 | `./bunnyctl -addr 127.0.0.1:8100` |

## API

`/api/health` · `/api/status` · `/api/eq/{dac,adc}` · `/api/preamp` ·
`/api/save` · `/api/factory` · `/api/registers` · `/api/raw` · `/api/presets`

```bash
curl -s localhost:8099/api/eq/dac
curl -s -X POST localhost:8099/api/save
```

## 开发

```bash
make test    # 单元测试
make vet
make build
```

协议、寄存器地图、固件布局见 [docs/PROTOCOL.md](docs/PROTOCOL.md)。

## License

MIT。非官方项目，与 TANCHJIM（天使吉米）无隶属关系；写 flash 有风险，动手前先看协议笔记里的备份方法。

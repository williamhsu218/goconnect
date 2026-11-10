# 直连地址导入与导出

入口：**线路配置 → 编辑线路 → 直连域名与 IP → 导入地址…**。

导入文件后先显示新增数、已有数和新增明细；确认后只合并到当前线路草稿，点击“保存线路”才写入配置。取消预览或放弃线路编辑不会改变已保存配置。相同地址自动跳过；如果已有域名的“包含子域名”设置不同，保留已有设置并提示。不会修改分流模式、应用名单、订阅、服务器、密码或其他线路。

**正在使用的线路仍为只读**，不能在连接期间导入或保存。导入文件可提前准备；断开后保存，下次连接使用。导出只读取编辑器中的地址（含未保存草稿），可用于备份或在另一条线路导入，不包含线路凭据。此入口为本次合并保留的未发布开发功能，需要后续正式升级才会出现在已安装 App 中；本次合并不替换运行中的 App。

## 文件格式

UTF-8 `.txt` 或 `.list`，最多 64 KB；每条线路合并后最多 256 个唯一地址。支持 BOM、空行、以 `#` 或 `//` 开头的整行注释。

```text
# 普通域名包含子域名；IPv4/IPv6 与 CIDR 使用现有地址校验
example.com
192.168.2.10
192.168.2.0/24
fd00:1234::/64

# 可选的 DIRECT 规则行；DOMAIN 仅匹配完整域名
DOMAIN,exact.example.org,DIRECT
DOMAIN-SUFFIX,example.net,DIRECT
IP-CIDR,192.168.3.0/24,DIRECT,no-resolve
IP-CIDR6,fd00:5678::/64,DIRECT
```

这是地址列表，不是完整 Clash/Surge 订阅导入。代理动作、`MATCH`、`GEOIP`、进程规则、脚本、URL、端口、默认路由、回环与组播会被拒绝。任一行无效、文件内同名域名范围冲突或超过上限，整次导入失败且不改草稿，错误指出行号。`no-resolve` 仅作为 IP 规则的兼容尾项接受；不会改变现有 IP 分流行为。

## 国内常用网站示例

[direct-domestic.txt](../examples/direct-domestic.txt) 是可编辑的保守起点，按购物、视频社区、网盘、生活出行分组。包含已用的九个网站域名以方便复用，导入时重复项跳过；不是完整的中国网站数据库，也不保证每个服务的所有 CDN、IP 直连下载或第三方登录都被覆盖。

新补充范围参考各服务官网：

- [京东](https://www.jd.com/)（页面的图片与证照链接使用 `360buyimg.com`）、[天猫](https://www.tmall.com/)。
- [哔哩哔哩](https://www.bilibili.com/)、[优酷](https://www.youku.com/)、[爱奇艺](https://www.iqiyi.com/)。
- [知乎](https://www.zhihu.com/)、[豆瓣](https://www.douban.com/)（页面资源使用 `doubanio.com`）。
- [百度网盘](https://wangpan.baidu.com/download)、[阿里云盘](https://www.alipan.com/)。
- [美团](https://www.meituan.com/)、[大众点评](https://www.dianping.com/)、[铁路 12306](https://www.12306.cn/)、[高德](https://www.amap.com/)。

不添加 `.cn` 整体、公共云后缀或共享 IP 段；不新增或删除 AI 服务的专用规则。现有广泛的企业域名规则保持原样，可能覆盖企业旗下的多种服务。测试检查新增清单不命中 Claude、OpenAI、Gemini、DeepSeek 等已列举的服务域名；这是静态匹配检查，不代表对所有 AI 的实时流量作了验收。

全局模式下，未命中直连规则的流量仍按原有全局策略处理。本地和 Tailscale 例外沿用现有实现。现有系统 DNS 策略在下次连接时为这些明确直连域名生成原网络解析规则；没有修改默认 DNS、TUN、路由优先级或代理内核。

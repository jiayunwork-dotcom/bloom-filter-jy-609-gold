# 项目一句话：做什么用的

## 构建 / 运行 / 测试

```text
go build ./...     # 编译
go run . add -f filter.bin -items a b c   # 构建/更新过滤器
go run . check -f filter.bin -item x      # 查询元素是否存在
go test ./...      # 测试
```

## 评测镜像

本目录评测专用文件（勿覆盖项目自带 Dockerfile/README）：

- `benzhi.Dockerfile`
- `build_benzhi_docker.sh`
- `BENZHI_README.md`（本文件）

两种架构都要构建并进容器验证：

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh <image-name> linux/arm64
./build_benzhi_docker.sh <image-name> linux/amd64
docker run -it <image-name>:latest
```

# Pixly EasyMode - 图像格式转换工具套件

> 🚀 **专为图像收藏家和效率追求者设计的高性能图像转换工具集**

Pixly EasyMode 是一套完整的图像格式转换工具，提供三种专业的转换器，帮助您将各种图像格式转换为现代、高效的图像格式。所有工具都采用"全自动模式"设计，无需复杂配置，智能识别文件类型并采用最优策略处理。

---

## 🎯 工具套件概览

### 📦 包含的工具

| 工具 | 功能 | 输入格式 | 输出格式 | 核心特色 |
|------|------|----------|----------|----------|
| **all2jxl** | 无损图像转换 | JPEG, PNG, GIF, WebP, AVIF, HEIF, BMP, TIFF, ICO, CUR | JPEG XL (.jxl) | 🔥 **真正的数学无损转换** |
| **static2avif** | 静态图像转换 | JPEG, PNG, BMP, TIFF | AVIF (.avif) | ⚡ **高压缩率静态图像** |
| **dynamic2avif** | 动态图像转换 | GIF, WebP, APNG | AVIF (.avif) | 🎬 **保留动画效果** |

---

## 🌟 核心优势

### 🧠 全自动智能处理
- **零配置使用**：所有工具都采用"全自动模式"，无需任何复杂配置
- **智能格式识别**：通过文件头部的"魔法字节"精确识别文件真实格式
- **最优策略选择**：根据文件类型自动选择最佳转换策略

### 🔒 安全可靠
- **事务性操作**：采用原子性操作，确保转换过程中不会损坏原始文件
- **多层验证机制**：转换后自动验证文件完整性和质量
- **智能错误恢复**：支持重试机制，处理临时故障

### ⚡ 高性能处理
- **多线程并发**：充分利用现代CPU的多核性能
- **智能资源管理**：避免系统过载，确保稳定运行
- **批量处理优化**：高效处理大量文件

### 📊 详细统计报告
- **实时进度显示**：清晰的处理进度和状态信息
- **详细日志记录**：完整的处理日志和统计信息
- **文件大小对比**：显示转换前后的文件大小变化

---

## 🛠️ 工具详细介绍

### 1. all2jxl - JPEG XL 无损转换器

**🎯 专为追求完美无损转换的用户设计**

#### 核心特性
- **真正的数学无损**：保证像素级别的完美无损转换
- **JPEG 无损重编码**：对 JPEG 文件进行无损重编码，享受约20%的体积减小
- **完整元数据保留**：保留所有 EXIF、XMP 等元数据信息
- **多层交叉验证**：确保转换结果的完美性
- **动画支持**：处理 GIF、WebP、APNG、AVIF、HEIF 动画并验证帧数

#### 支持的格式
- **输入**：JPEG, PNG, GIF, WebP, AVIF, HEIF, BMP, TIFF, ICO, CUR
- **输出**：JPEG XL (.jxl)

#### 使用场景
- 图像收藏管理
- 专业摄影后期处理
- 需要完美无损转换的场景

#### 高级功能
- **魔法字节检测**：忽略文件扩展名，使用实际文件头识别
- **动画帧验证**：确保完整帧转换
- **多层级元数据迁移**：EXIF、XMP、IPTC 数据保留
- **采样模式**：使用中等大小文件进行测试
- **复制模式**：处理副本避免修改原始文件

### 2. static2avif - 静态图像 AVIF 转换器

**🎯 专为网站优化和存储空间节省设计**

#### 核心特性
- **高压缩率**：相比 JPEG/PNG 显著减小文件大小
- **现代格式支持**：支持 HDR、宽色域、透明度等现代特性
- **视觉无损转换**：在保持视觉质量的同时优化文件大小
- **快速处理**：优化的编码算法，快速完成转换

#### 支持的格式
- **输入**：JPEG, PNG, BMP, TIFF
- **输出**：AVIF (.avif)

#### 使用场景
- 网站图片优化
- 移动应用资源优化
- 存储空间优化

#### 技术实现
- **FFmpeg 集成**：使用 libsvtav1 编码器进行高质量 AVIF 编码
- **智能质量控制**：根据质量参数动态调整 CRF 值
- **并发处理**：多线程转换与资源管理

### 3. dynamic2avif - 动态图像 AVIF 转换器

**🎯 专为动态图像优化设计**

#### 核心特性
- **动画保留**：完整保留原始动画效果
- **高压缩率**：相比 GIF/WebP 显著减小文件大小
- **现代动画支持**：支持复杂的动画效果
- **智能动画检测**：自动识别和验证动画特性

#### 支持的格式
- **输入**：GIF, WebP, APNG
- **输出**：AVIF (.avif)

#### 使用场景
- 网站动画优化
- 社交媒体内容优化
- 动态图像收藏管理

#### 技术实现
- **动画检测**：自动检测动画内容
- **帧验证**：确保完整动画转换
- **质量优化**：平衡压缩与视觉质量

---

## 🚀 快速开始

### 系统要求
- **Go 1.19+**：用于构建工具
- **FFmpeg 4.0+**：用于图像转换（static2avif, dynamic2avif）
- **cjxl/djxl**：JPEG XL 编码器/解码器（all2jxl）
- **exiftool**：元数据处理工具（all2jxl）

### 安装依赖

#### macOS
```bash
# 安装 FFmpeg
brew install ffmpeg

# 安装 JPEG XL 工具
brew install jpeg-xl

# 安装 exiftool
brew install exiftool
```

#### Ubuntu/Debian
```bash
# 安装 FFmpeg
sudo apt install ffmpeg

# 安装 JPEG XL 工具
sudo apt install libjxl-tools

# 安装 exiftool
sudo apt install exiftool
```

### 构建工具

每个工具都提供了构建脚本，可以快速构建：

```bash
# 构建 all2jxl
cd all2jxl && ./build.sh

# 构建 static2avif
cd static2avif && ./build.sh

# 构建 dynamic2avif
cd dynamic2avif && ./build.sh
```

---

## 📖 使用指南

### all2jxl 使用示例

```bash
# 基本用法：无损转换整个目录
./all2jxl -dir "/path/to/your/images"

# 高级用法：多线程处理
./all2jxl -dir "/path/to/your/images" -workers 8

# 试运行模式：预览将要处理的文件
./all2jxl -dir "/path/to/your/images" -dry-run

# 复制模式：处理副本避免修改原始文件
./all2jxl -dir "/path/to/your/images" -copy

# 采样模式：测试10个中等大小文件
./all2jxl -dir "/path/to/your/images" -sample 10

# 跳过已存在的文件
./all2jxl -dir "/path/to/your/images" -skip-exist

# 高质量验证
./all2jxl -dir "/path/to/your/images" -verify strict
```

### static2avif 使用示例

```bash
# 基本转换
./static2avif -input /path/to/images -output /path/to/avif/output

# 高质量转换
./static2avif -input /input -output /output -quality 80 -speed 5

# 限制并发数
./static2avif -input /input -output /output -workers 4

# 跳过已存在的文件
./static2avif -input /input -output /output -skip-exist
```

### dynamic2avif 使用示例

```bash
# 基本转换
./dynamic2avif -input /path/to/images -output /path/to/avif/output

# 高质量转换
./dynamic2avif -input /input -output /output -quality 80 -speed 5

# 跳过已存在的文件
./dynamic2avif -input /input -output /output -skip-exist
```

---

## 🔧 高级配置

### 性能优化建议

1. **线程数配置**
   - 建议设置为 CPU 核心数的 1-2 倍
   - 避免设置过高导致系统过载

2. **质量参数调优**
   - **all2jxl**：使用默认设置即可获得完美无损转换
   - **static2avif/dynamic2avif**：质量 50-80 之间平衡文件大小和质量

3. **内存使用优化**
   - 大文件处理时适当减少并发数
   - 监控系统资源使用情况

### 错误处理

所有工具都支持：
- **重试机制**：自动重试失败的转换
- **超时控制**：避免长时间阻塞
- **优雅退出**：支持 Ctrl+C 安全退出

---

## 📊 性能对比

### 文件大小优化效果

| 原始格式 | 目标格式 | 平均压缩率 | 质量保持 |
|----------|----------|------------|----------|
| JPEG → JPEG XL | 约 20% 减小 | 完美无损 |
| PNG → AVIF | 约 50-70% 减小 | 视觉无损 |
| GIF → AVIF | 约 60-80% 减小 | 动画保留 |

### 处理速度

- **all2jxl**：中等速度，注重质量和验证
- **static2avif**：快速处理，适合批量转换
- **dynamic2avif**：中等速度，保留动画质量

---

## 🛡️ 安全特性

### 数据安全
- **原子性操作**：转换失败时自动回滚
- **原始文件保护**：转换过程中原始文件始终安全
- **验证机制**：转换后自动验证文件完整性

### 错误恢复
- **重试机制**：自动重试失败的转换
- **部分失败处理**：单个文件失败不影响整体处理
- **详细日志**：完整的错误信息和处理状态

---

## 📁 项目结构

```
easymode/
├── all2jxl/                    # JPEG XL 无损转换器
│   ├── main.go
│   ├── README.md
│   ├── PROCESSING_FLOW.md      # 技术处理流程文档
│   ├── build.sh
│   ├── go.mod
│   ├── go.sum
│   ├── src/
│   │   └── main.go
│   └── all2jxl                 # 编译后的可执行文件
├── static2avif/               # 静态图像 AVIF 转换器
│   ├── main.go
│   ├── README.md
│   ├── FEATURES.md             # 功能文档
│   ├── PROCESSING_FLOW.md      # 技术处理流程
│   ├── build.sh
│   ├── test.sh
│   ├── go.mod
│   ├── go.sum
│   └── static2avif             # 编译后的可执行文件
├── dynamic2avif/              # 动态图像 AVIF 转换器
│   ├── main.go
│   ├── README.md
│   ├── FEATURES.md             # 功能文档
│   ├── PROCESSING_FLOW.md      # 技术处理流程
│   ├── build.sh
│   ├── test.sh
│   ├── go.mod
│   ├── go.sum
│   └── dynamic2avif           # 编译后的可执行文件
├── README.md                   # 英文版总览文档
└── README_ZH.md                # 本文件 - 中文版总览文档
```

---

## 🤝 贡献指南

欢迎贡献代码、报告问题或提出改进建议！

### 开发环境设置
1. 克隆项目仓库
2. 安装必要的依赖
3. 运行测试确保环境正常

### 提交规范
- 使用清晰的提交信息
- 确保代码通过所有测试
- 更新相关文档

---

## 📄 许可证

本项目采用 MIT 许可证。详见各子项目的 LICENSE 文件。

---

## 🆘 支持与帮助

### 常见问题
1. **依赖安装问题**：确保所有依赖工具正确安装并在 PATH 中
2. **权限问题**：确保对目标目录有读写权限
3. **内存不足**：减少并发线程数或处理文件大小

### 获取帮助
- 查看各工具的详细 README 文档
- 检查日志文件获取详细错误信息
- 使用 `-dry-run` 模式预览操作

---

**🎉 开始使用 Pixly EasyMode，让图像转换变得简单高效！**

---

## 🌐 语言版本

- [English README](README.md) - 英文版本总览文档
- [中文版 README](README_ZH.md) - 中文版本总览文档
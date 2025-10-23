# Pixly EasyMode - Image Format Conversion Tool Suite

> 🚀 **High-performance image conversion tools designed for image collectors and efficiency seekers**

Pixly EasyMode is a complete suite of image format conversion tools, providing three professional converters to help you convert various image formats to modern, efficient image formats. All tools are designed with "full-automatic mode" - no complex configuration required, intelligently identifying file types and applying optimal processing strategies.

---

## 🎯 Tool Suite Overview

### 📦 Included Tools

| Tool | Function | Input Formats | Output Format | Key Features |
|------|----------|---------------|---------------|--------------|
| **all2jxl** | Lossless Image Conversion | JPEG, PNG, GIF, WebP, AVIF, HEIF, BMP, TIFF, ICO, CUR | JPEG XL (.jxl) | 🔥 **True mathematical lossless conversion** |
| **static2avif** | Static Image Conversion | JPEG, PNG, BMP, TIFF | AVIF (.avif) | ⚡ **High compression static images** |
| **dynamic2avif** | Dynamic Image Conversion | GIF, WebP, APNG | AVIF (.avif) | 🎬 **Preserves animation effects** |

---

## 🌟 Core Advantages

### 🧠 Full-Automatic Intelligent Processing
- **Zero-configuration usage**: All tools use "full-automatic mode" with no complex configuration required
- **Intelligent format recognition**: Precisely identifies real file formats through "magic bytes" in file headers
- **Optimal strategy selection**: Automatically selects the best conversion strategy based on file type

### 🔒 Safe and Reliable
- **Transactional operations**: Uses atomic operations to ensure original files are never damaged during conversion
- **Multi-layer verification**: Automatically verifies file integrity and quality after conversion
- **Intelligent error recovery**: Supports retry mechanisms to handle temporary failures

### ⚡ High-Performance Processing
- **Multi-threaded concurrency**: Fully utilizes modern CPU multi-core performance
- **Intelligent resource management**: Prevents system overload, ensures stable operation
- **Batch processing optimization**: Efficiently processes large numbers of files

### 📊 Detailed Statistical Reports
- **Real-time progress display**: Clear processing progress and status information
- **Detailed logging**: Complete processing logs and statistical information
- **File size comparison**: Shows file size changes before and after conversion

---

## 🛠️ Detailed Tool Introduction

### 1. all2jxl - JPEG XL Lossless Converter

**🎯 Designed for users pursuing perfect lossless conversion**

#### Core Features
- **True mathematical lossless**: Guarantees pixel-perfect lossless conversion
- **JPEG lossless re-encoding**: Performs lossless re-encoding on JPEG files, enjoying ~20% size reduction
- **Complete metadata preservation**: Preserves all EXIF, XMP and other metadata information
- **Multi-layer cross-verification**: Ensures perfect conversion results
- **Animation support**: Handles GIF, WebP, APNG, AVIF, HEIF animations with frame verification

#### Supported Formats
- **Input**: JPEG, PNG, GIF, WebP, AVIF, HEIF, BMP, TIFF, ICO, CUR
- **Output**: JPEG XL (.jxl)

#### Use Cases
- Image collection management
- Professional photography post-processing
- Scenarios requiring perfect lossless conversion

#### Advanced Features
- **Magic byte detection**: Ignores file extensions, uses actual file headers
- **Animation frame verification**: Ensures complete frame conversion
- **Multi-level metadata migration**: EXIF, XMP, IPTC data preservation
- **Sampling mode**: Test with medium-sized files
- **Copy mode**: Process copies to avoid original file modification

### 2. static2avif - Static Image AVIF Converter

**🎯 Designed for website optimization and storage space saving**

#### Core Features
- **High compression ratio**: Significantly reduces file size compared to JPEG/PNG
- **Modern format support**: Supports HDR, wide color gamut, transparency and other modern features
- **Visually lossless conversion**: Optimizes file size while maintaining visual quality
- **Fast processing**: Optimized encoding algorithms for quick conversion

#### Supported Formats
- **Input**: JPEG, PNG, BMP, TIFF
- **Output**: AVIF (.avif)

#### Use Cases
- Website image optimization
- Mobile app resource optimization
- Storage space optimization

#### Technical Implementation
- **FFmpeg integration**: Uses libsvtav1 encoder for high-quality AVIF encoding
- **Smart quality control**: Dynamic CRF adjustment based on quality parameters
- **Concurrent processing**: Multi-threaded conversion with resource management

### 3. dynamic2avif - Dynamic Image AVIF Converter

**🎯 Designed for dynamic image optimization**

#### Core Features
- **Animation preservation**: Completely preserves original animation effects
- **High compression ratio**: Significantly reduces file size compared to GIF/WebP
- **Modern animation support**: Supports complex animation effects
- **Intelligent animation detection**: Automatically identifies and verifies animation characteristics

#### Supported Formats
- **Input**: GIF, WebP, APNG
- **Output**: AVIF (.avif)

#### Use Cases
- Website animation optimization
- Social media content optimization
- Dynamic image collection management

#### Technical Implementation
- **Animation detection**: Automatic detection of animated content
- **Frame verification**: Ensures complete animation conversion
- **Quality optimization**: Balanced compression and visual quality

---

## 🚀 Quick Start

### System Requirements
- **Go 1.19+**: For building tools
- **FFmpeg 4.0+**: For image conversion (static2avif, dynamic2avif)
- **cjxl/djxl**: JPEG XL encoder/decoder (all2jxl)
- **exiftool**: Metadata processing tool (all2jxl)

### Installing Dependencies

#### macOS
```bash
# Install FFmpeg
brew install ffmpeg

# Install JPEG XL tools
brew install jpeg-xl

# Install exiftool
brew install exiftool
```

#### Ubuntu/Debian
```bash
# Install FFmpeg
sudo apt install ffmpeg

# Install JPEG XL tools
sudo apt install libjxl-tools

# Install exiftool
sudo apt install exiftool
```

### Building Tools

Each tool provides a build script for quick building:

```bash
# Build all2jxl
cd all2jxl && ./build.sh

# Build static2avif
cd static2avif && ./build.sh

# Build dynamic2avif
cd dynamic2avif && ./build.sh
```

---

## 📖 Usage Guide

### all2jxl Usage Examples

```bash
# Basic usage: Lossless conversion of entire directory
./all2jxl -dir "/path/to/your/images"

# Advanced usage: Multi-threaded processing
./all2jxl -dir "/path/to/your/images" -workers 8

# Dry-run mode: Preview files to be processed
./all2jxl -dir "/path/to/your/images" -dry-run

# Copy mode: Process copies to avoid modifying originals
./all2jxl -dir "/path/to/your/images" -copy

# Sampling mode: Test with 10 medium-sized files
./all2jxl -dir "/path/to/your/images" -sample 10

# Skip existing files
./all2jxl -dir "/path/to/your/images" -skip-exist

# High-quality verification
./all2jxl -dir "/path/to/your/images" -verify strict
```

### static2avif Usage Examples

```bash
# Basic conversion
./static2avif -input /path/to/images -output /path/to/avif/output

# High-quality conversion
./static2avif -input /input -output /output -quality 80 -speed 5

# Limit concurrency
./static2avif -input /input -output /output -workers 4

# Skip existing files
./static2avif -input /input -output /output -skip-exist
```

### dynamic2avif Usage Examples

```bash
# Basic conversion
./dynamic2avif -input /path/to/images -output /path/to/avif/output

# High-quality conversion
./dynamic2avif -input /input -output /output -quality 80 -speed 5

# Skip existing files
./dynamic2avif -input /input -output /output -skip-exist
```

---

## 🔧 Advanced Configuration

### Performance Optimization Recommendations

1. **Thread Configuration**
   - Recommended to set to 1-2 times the number of CPU cores
   - Avoid setting too high to prevent system overload

2. **Quality Parameter Tuning**
   - **all2jxl**: Use default settings for perfect lossless conversion
   - **static2avif/dynamic2avif**: Balance file size and quality between 50-80

3. **Memory Usage Optimization**
   - Reduce concurrency for large file processing
   - Monitor system resource usage

### Error Handling

All tools support:
- **Retry mechanism**: Automatically retry failed conversions
- **Timeout control**: Avoid long-term blocking
- **Graceful exit**: Support Ctrl+C for safe exit

---

## 📊 Performance Comparison

### File Size Optimization Effects

| Original Format | Target Format | Average Compression | Quality Retention |
|-----------------|---------------|-------------------|-------------------|
| JPEG → JPEG XL | ~20% reduction | Perfect lossless |
| PNG → AVIF | ~50-70% reduction | Visually lossless |
| GIF → AVIF | ~60-80% reduction | Animation preserved |

### Processing Speed

- **all2jxl**: Medium speed, focuses on quality and verification
- **static2avif**: Fast processing, suitable for batch conversion
- **dynamic2avif**: Medium speed, preserves animation quality

---

## 🛡️ Security Features

### Data Security
- **Atomic operations**: Automatic rollback on conversion failure
- **Original file protection**: Original files remain safe throughout conversion process
- **Verification mechanism**: Automatic file integrity verification after conversion

### Error Recovery
- **Retry mechanism**: Automatically retry failed conversions
- **Partial failure handling**: Single file failure doesn't affect overall processing
- **Detailed logging**: Complete error information and processing status

---

## 📁 Project Structure

```
easymode/
├── all2jxl/                    # JPEG XL Lossless Converter
│   ├── main.go
│   ├── README.md
│   ├── PROCESSING_FLOW.md      # Technical processing flow documentation
│   ├── build.sh
│   ├── go.mod
│   ├── go.sum
│   ├── src/
│   │   └── main.go
│   └── all2jxl                 # Compiled executable
├── static2avif/               # Static Image AVIF Converter
│   ├── main.go
│   ├── README.md
│   ├── FEATURES.md             # Feature documentation
│   ├── PROCESSING_FLOW.md      # Technical processing flow
│   ├── build.sh
│   ├── test.sh
│   ├── go.mod
│   ├── go.sum
│   └── static2avif             # Compiled executable
├── dynamic2avif/              # Dynamic Image AVIF Converter
│   ├── main.go
│   ├── README.md
│   ├── FEATURES.md             # Feature documentation
│   ├── PROCESSING_FLOW.md      # Technical processing flow
│   ├── build.sh
│   ├── test.sh
│   ├── go.mod
│   ├── go.sum
│   └── dynamic2avif           # Compiled executable
├── README.md                   # This file - Overview document (English)
└── README_ZH.md                # Chinese version overview document
```

---

## 🤝 Contributing Guide

Welcome to contribute code, report issues, or suggest improvements!

### Development Environment Setup
1. Clone the project repository
2. Install necessary dependencies
3. Run tests to ensure environment is working

### Commit Standards
- Use clear commit messages
- Ensure code passes all tests
- Update relevant documentation

---

## 📄 License

This project is licensed under the MIT License. See LICENSE files in each sub-project for details.

---

## 🆘 Support and Help

### Common Issues
1. **Dependency installation issues**: Ensure all dependency tools are correctly installed and in PATH
2. **Permission issues**: Ensure read/write permissions for target directories
3. **Insufficient memory**: Reduce concurrency threads or processing file size

### Getting Help
- Check detailed README documentation for each tool
- Check log files for detailed error information
- Use `-dry-run` mode to preview operations

---

**🎉 Start using Pixly EasyMode and make image conversion simple and efficient!**

---

## 🌐 Language Versions

- [English README](README.md) - English version overview document
- [中文版 README](README_ZH.md) - 中文版本总览文档
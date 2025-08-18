# MediaForge v15.0.0 - 高性能媒体转换工具⬇️
	
# GoMediaForge v15.0.0 - High-Performance Media Converter⬆️



-------------------------------------------------------------------------------------------------------------------------------------------------------------------

欢迎使用 GoMediaForge！本工具采用 Go 语言重构，专为 macOS 优化，提供高效、可靠的图像和视频格式转换。支持 AVIF、JXL、HEVC 和 AV1 格式，适用于存档、日常使用或自动化任务。

Welcome to GoMediaForge! This script, rewritten in Go, is optimized for macOS, offering efficient and reliable batch conversion for images and videos. It supports AVIF, JXL, HEVC, and AV1 formats, ideal for archiving, daily use, or automated workflows.

核心特性 | Key Features





多种模式：质量模式（追求极致画质）、效率模式（平衡画质与体积）、自动模式（智能选择）。
Multiple Modes: Quality mode (maximum fidelity), Efficiency mode (balanced quality/size), Auto mode (smart selection).



高性能：并发处理，充分利用多核 CPU，支持 macOS VideoToolbox 硬件加速。
High Performance: Concurrent processing leveraging multi-core CPUs, with macOS VideoToolbox hardware acceleration.



智能跳过：自动识别并跳过 Live Photo、空间图像及不支持的文件。
Smart Skipping: Automatically skips Live Photos, spatial images, and unsupported files.



备份与续传：支持文件备份，断点续传减少重复处理。
Backup & Resume: Supports file backups and resumable processing to avoid redundant work.



元数据保留：保留原始文件元数据（如 EXIF）。
Metadata Preservation: Retains original file metadata (e.g., EXIF).



详细日志：生成详细日志和报告，记录转换结果和空间节省。
Detailed Logging: Generates comprehensive logs and reports, tracking conversion results and space savings.




# 安装依赖 | Install dependencies
	#安装与使用 | Installation & Usage
	brew install ffmpeg imagemagick jpeg-xl exiftool
# 运行脚本 | Run the script
	go run main.go --dir <目标文件夹> --mode auto --jobs 4 --hwaccel true

# 依赖 | Dependencies 
	Core: ffmpeg, imagemagick, exiftool
	Optional: cjxl（JXL 转换 | for JXL conversion），libsvtav1（AV1 编码 | for AV1 encoding）

#  Logs与位置: 
	<目标文件夹>/<模式>_conversion_<时间戳>.txt

# 适合场景 | Use Cases

批量转换照片和视频以节省存储空间。
Batch convert photos and videos to save storage space.

将传统格式（PNG、JPG、MP4）转为现代格式（AVIF、JXL、HEVC）。
Transform legacy formats (PNG, JPG, MP4) to modern formats (AVIF, JXL, HEVC).



存档高质量媒体或优化日常分享。
Archive high-quality media or optimize for everyday sharing.

# 免责声明 | Disclaimer

本脚本仅限个人使用，经自用转换测试，未经过广泛场景验证。可能存在未发现的 bug，问题修复可能不及时或无响应。建议具备一定开发经验的用户自行调试和修复问题，使用前请备份重要文件。

This script is intended for personal use and has been tested in limited scenarios. It may contain undiscovered bugs, and issue resolution may be delayed or unresponsive. Users with some development experience are recommended to debug and fix issues themselves. Please back up important files before use.

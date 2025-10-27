package version

// Version 信息统一管理
// 这是项目中唯一的版本号定义文件，所有其他文件都应该引用这里的版本信息
const (
	// Major 主版本号
	Major = 1
	// Minor 次版本号
	Minor = 65
	// Patch 修订版本号
	Patch = 7
	// Build 构建版本号（修复context canceled错误处理）
	Build = 5

	// Version 完整版本号（不带v前缀）
	Version = "1.65.7.5"
	// VersionWithPrefix 带v前缀的版本号
	VersionWithPrefix = "v1.65.7.5"
	// TestFrameworkVersion 测试框架版本号
	TestFrameworkVersion = "v1.0.0"
)

// BuildInfo 构建信息
var (
	// BuildTime 构建时间（通过ldflags设置）
	BuildTime = "unknown"
	// GitCommit Git提交哈希（通过ldflags设置）
	GitCommit = "unknown"
)

// GetVersion 获取版本号（不带前缀）
func GetVersion() string {
	return Version
}

// GetVersionWithPrefix 获取带前缀的版本号
func GetVersionWithPrefix() string {
	return VersionWithPrefix
}

// GetTestFrameworkVersion 获取测试框架版本号
func GetTestFrameworkVersion() string {
	return TestFrameworkVersion
}

// GetBuildTime 获取构建时间
func GetBuildTime() string {
	return BuildTime
}

// GetGitCommit 获取Git提交哈希
func GetGitCommit() string {
	return GitCommit
}

// SetBuildInfo 设置构建信息（用于构建时通过ldflags设置）
func SetBuildInfo(buildTime, gitCommit string) {
	BuildTime = buildTime
	GitCommit = gitCommit
}

// GetFullVersionInfo 获取完整版本信息
func GetFullVersionInfo() string {
	return VersionWithPrefix + " (built at " + BuildTime + ", commit " + GitCommit + ")"
}

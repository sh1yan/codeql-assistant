package engine

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CodeQLConfig 保存 CodeQL 分析运行的所有配置。
type CodeQLConfig struct {
	Name         string `json:"name,omitempty"`
	BinaryPath   string `json:"binary_path"`
	QueryPath    string `json:"query_path"`
	DatabasePath string `json:"database_path"`
	SourceRoot   string `json:"source_root"`
	Language     string `json:"language"`
	OutputFormat string `json:"output_format"`
	OutputFile   string `json:"output_file"`
	RamLimit     int    `json:"ram_limit"`
	Threads      int    `json:"threads"`
	ExtraArgs    string `json:"extra_args"`
}

// DefaultConfig 返回一个包含合理默认值的配置。
func DefaultConfig() CodeQLConfig {
	return CodeQLConfig{
		Name:         "",
		BinaryPath:   "",
		QueryPath:    "",
		DatabasePath: "",
		SourceRoot:   "",
		Language:     "java",
		OutputFormat: "sarifv2.1.0",
		OutputFile:   "",
		RamLimit:     4096,
		Threads:      4,
		ExtraArgs:    "",
	}
}

// SupportedLanguages 返回 CodeQL 可分析的语言列表。
func SupportedLanguages() []string {
	return []string{
		"java", "python", "javascript", "typescript",
		"cpp", "csharp", "go", "ruby", "swift", "kotlin",
	}
}

// SupportedFormats 返回分析结果支持的输出格式列表。
func SupportedFormats() []string {
	return []string{
		"sarifv2.1.0", "sarif-latest", "csv", "json",
		"bqrs", "graphs", "dgml",
	}
}

// SaveToFile 将配置持久化到 JSON 文件中。
func (c CodeQLConfig) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadConfigFromFile 从 JSON 文件中读取配置。
func LoadConfigFromFile(path string) (CodeQLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	var cfg CodeQLConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	return cfg, nil
}

// ConfigDir 返回用于存储配置文件的目录。
func ConfigDir() string {
	return filepath.Join(AppDataDir(), "profiles")
}

// ListProfiles 返回配置目录中可用的配置文件名称列表。
func ListProfiles() []string {
	dir := ConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var profiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			name := e.Name()[:len(e.Name())-5]
			profiles = append(profiles, name)
		}
	}
	sort.Strings(profiles)
	return profiles
}

// ProfilePath 返回指定名称配置文件的完整路径。
func ProfilePath(name string) string {
	return filepath.Join(ConfigDir(), name+".json")
}

// DeleteProfile 删除指定名称的配置文件。
func DeleteProfile(name string) error {
	path := ProfilePath(name)
	return os.Remove(path)
}

// GetRandomQuote 读取预录列表，去重后随机抽取一条返回
func GetRandomQuote() string {
	quotes := []string{
		"每一行代码背后，都藏着值得被发现的真相。",
		"漏洞不会自己说话，但查询可以替它开口。",
		"开源的代码是透明的，但安全需要有人去看见。",
		"你查的每一行，都是社区信任的一分。",
		"别人看代码找功能，你看代码找谎言。",
		"那个你没报出来的漏洞，其实你也救了很多人。",
		"False positive 第一百次，但第一百零一次你还是会点进去。",
		"别人眼里的能跑就行，是你眼里的必须查清楚。",
		"没有你的名字，但有你拦下来的漏洞。",
		"看到用户输入，手指已经比脑子先动了。",
		"第十个误报之后，第十一个你还是点了进去。",
		"三点十七分，咖啡凉了，但调用链终于通了。",
		"每次交报告前，你都比开发多问自己一遍：还有吗？",
		"最漂亮的一行代码，是你让它没机会被利用。",
		"整个仓库，只有你读过这一段的每一行。",
		"你和代码之间，有一种开发不懂的相处方式。",
		"你查的不是代码，是很多人睡不着的理由。",
		"鼠标悬停在 eval 上的时候，你的心跳漏了一拍。",
		"明明已经扫过三遍，睡前还是忍不住再翻一遍。",
		"开发说没问题，你说我再看看。",
		"一个漏洞从发现到修复，中间隔了三个月和十七封邮件。",
		"逛 GitHub 像逛菜市场，顺手就审计起来了。",
		"那个你随手修掉的边界条件，后来真的有人触发了。",
		"家人问你天天对着代码找什么，你说找麻烦。",
		"明明可以报低危，但你就是想知道能不能再深一层。",
		"每次提交报告前，都要把 PoC 再跑一遍，哪怕已经跑了二十遍。",
		"你不认识写代码的人，但你可能比他自己更了解他的程序。",
		"如果上次没多看那一眼，现在上新闻的可能就是这个项目。",
		"你的工作没有发布会，但阻止了很多发布会。",
		"你拦下来的每一个漏洞，都是某个普通人平静的一天。",
		"变量名从 temp 改成 tmp，你都要停下来多看两眼。",
		"还没走到 sink，你已经知道哪里会漏。",
		"开发说这不可能被触发，你花了两小时写了个 PoC。",
		"看到登录框就手痒，看到文件上传就精神。",
		"你的简历上写着发现漏洞，没写的是那些白跑的三百个小时。",
		"别人问这个严重吗，你说让我再确认一下，其实你已经确认了五遍。",
		"开发群里有人说哪个傻子写的这代码，你默默关掉窗口，深藏功与名。",
		"眼睛花了，脑子慢了，但手指还在翻下一页。",
		"你可以不报，但你知道如果你不报，可能没人会报。",
		"朋友让你帮忙看个网站，你顺手就按了 F12。",
		"你和漏洞之间，隔着一万行代码和一杯凉透的咖啡。",
		"最好的结果不是发现漏洞，是发现这里真的没有漏洞。",
		"你读过很多人的代码，但很少有人读过你的。",
		"每一行你认真看过的代码，都在某个地方保护着某个你不认识的人。",
	}

	// 去重：利用 map 的键唯一性
	seen := make(map[string]bool)
	unique := make([]string, 0, len(quotes))
	for _, q := range quotes {
		if !seen[q] {
			seen[q] = true
			unique = append(unique, q)
		}
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return unique[r.Intn(len(unique))]
}

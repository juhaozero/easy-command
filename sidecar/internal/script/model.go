package script

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"easy-command/sidecar/internal/secret"
)

type CLI struct {
	Mode    string `json:"mode"`
	EnvName string `json:"envName,omitempty"`
	Flag    string `json:"flag,omitempty"`
}

type Choice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Param struct {
	Key            string          `json:"key"`
	Label          string          `json:"label"`
	Type           string          `json:"type"`
	Required       bool            `json:"required"`
	Default        json.RawMessage `json:"default,omitempty"`
	Choices        []Choice        `json:"choices,omitempty"`
	PasswordStored *bool           `json:"passwordStored,omitempty"`
	UpdatedAt      string          `json:"updatedAt,omitempty"`
	SecretBinding  string          `json:"secretBinding,omitempty"`
	MaskInLog      bool            `json:"maskInLog"`
	CLI            CLI             `json:"cli"`
}

type Preset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Values      map[string]string `json:"values"`
}

type Fields struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Enabled         bool     `json:"enabled"`
	Dangerous       bool     `json:"dangerous"`
	WorkingDir      string   `json:"workingDir,omitempty"`
	Params          []Param  `json:"params"`
	Presets         []Preset `json:"presets,omitempty"`
	DefaultPresetID string   `json:"defaultPresetId,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type Meta struct {
	Fields
	ID        string `json:"id"`
	FileName  string `json:"fileName"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	secrets   []secret.Entry
}

type Summary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Dangerous bool   `json:"dangerous"`
	UpdatedAt string `json:"updatedAt"`
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func choiceValues(choices []Choice) map[string]bool {
	out := make(map[string]bool, len(choices))
	for _, choice := range choices {
		out[choice.Value] = true
	}
	return out
}

func defaultAsString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	case float64:
		return fmt.Sprint(typed), true
	case bool:
		return fmt.Sprint(typed), true
	default:
		return "", false
	}
}

func (f *Fields) validate() error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len(f.Name) > 255 || strings.ContainsAny(f.Name, "/\\\x00\r\n") {
		return fail("INVALID_PARAM", "脚本名称无效")
	}
	if f.Params == nil {
		f.Params = []Param{}
	}
	if f.Presets == nil {
		f.Presets = []Preset{}
	}
	keys := make(map[string]bool)
	paramsByKey := make(map[string]*Param)
	for i := range f.Params {
		p := &f.Params[i]
		p.Key = strings.TrimSpace(p.Key)
		p.Label = strings.TrimSpace(p.Label)
		if p.Key == "" || p.Label == "" || keys[p.Key] {
			return fail("INVALID_PARAM", "参数键和标签不能为空，参数键必须唯一")
		}
		keys[p.Key] = true
		paramsByKey[p.Key] = p
		switch p.Type {
		case "string", "number", "boolean", "file", "dir":
			if len(p.Default) > 0 {
				var value any
				if err := json.Unmarshal(p.Default, &value); err != nil {
					return fail("INVALID_PARAM", "参数默认值无效")
				}
				valid := false
				switch value.(type) {
				case string:
					valid = p.Type == "string" || p.Type == "file" || p.Type == "dir"
				case float64:
					valid = p.Type == "number"
				case bool:
					valid = p.Type == "boolean"
				}
				if !valid {
					return fail("INVALID_PARAM", "参数默认值类型不匹配")
				}
			}
		case "password":
			if len(p.Default) > 0 {
				return fail("INVALID_PARAM", "密码参数不能保存默认值")
			}
			if len(p.Choices) > 0 {
				return fail("INVALID_PARAM", "密码参数不能设置选项")
			}
			p.MaskInLog = true
		default:
			return fail("INVALID_PARAM", "未知参数类型")
		}
		if len(p.Choices) > 0 {
			if p.Type != "string" && p.Type != "number" && p.Type != "boolean" {
				return fail("INVALID_PARAM", "仅字符串/数字/布尔参数可设置选项")
			}
			seen := make(map[string]bool)
			cleaned := make([]Choice, 0, len(p.Choices))
			for _, choice := range p.Choices {
				label := strings.TrimSpace(choice.Label)
				value := strings.TrimSpace(choice.Value)
				if label == "" || value == "" {
					return fail("INVALID_PARAM", "参数选项的名称和值不能为空")
				}
				if seen[value] {
					return fail("INVALID_PARAM", "参数选项值必须唯一")
				}
				seen[value] = true
				cleaned = append(cleaned, Choice{Label: label, Value: value})
			}
			p.Choices = cleaned
			if text, ok := defaultAsString(p.Default); ok && !seen[text] {
				return fail("INVALID_PARAM", "参数默认值必须是已有选项之一")
			}
		}
		p.PasswordStored = nil
		p.UpdatedAt = ""
		switch p.CLI.Mode {
		case "arg":
		case "env":
			if strings.TrimSpace(p.CLI.EnvName) == "" {
				return fail("INVALID_PARAM", "环境变量名称不能为空")
			}
		case "flag":
			if strings.TrimSpace(p.CLI.Flag) == "" {
				return fail("INVALID_PARAM", "标志名称不能为空")
			}
		default:
			return fail("INVALID_PARAM", "未知参数模式")
		}
	}

	presetIDs := make(map[string]bool)
	cleanedPresets := make([]Preset, 0, len(f.Presets))
	for i := range f.Presets {
		preset := f.Presets[i]
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Name = strings.TrimSpace(preset.Name)
		preset.Description = strings.TrimSpace(preset.Description)
		if preset.ID == "" || preset.Name == "" {
			return fail("INVALID_PARAM", "方案 ID 和名称不能为空")
		}
		if presetIDs[preset.ID] {
			return fail("INVALID_PARAM", "方案 ID 必须唯一")
		}
		presetIDs[preset.ID] = true
		if preset.Values == nil {
			preset.Values = map[string]string{}
		}
		cleanedValues := make(map[string]string, len(preset.Values))
		for key, raw := range preset.Values {
			param, ok := paramsByKey[key]
			if !ok {
				return fail("INVALID_PARAM", "方案引用了未知参数："+key)
			}
			if param.Type == "password" {
				return fail("INVALID_PARAM", "方案不能包含密码参数")
			}
			value := strings.TrimSpace(raw)
			if value == "" {
				if param.Required {
					return fail("INVALID_PARAM", "方案缺少必填参数："+param.Label)
				}
				continue
			}
			if len(param.Choices) > 0 && !choiceValues(param.Choices)[value] {
				return fail("INVALID_PARAM", "方案参数必须选自已有选项："+param.Label)
			}
			cleanedValues[key] = value
		}
		for _, param := range f.Params {
			if param.Type == "password" || !param.Required {
				continue
			}
			if cleanedValues[param.Key] == "" {
				return fail("INVALID_PARAM", "方案缺少必填参数："+param.Label)
			}
		}
		preset.Values = cleanedValues
		cleanedPresets = append(cleanedPresets, preset)
	}
	f.Presets = cleanedPresets
	f.DefaultPresetID = strings.TrimSpace(f.DefaultPresetID)
	if f.DefaultPresetID != "" && !presetIDs[f.DefaultPresetID] {
		return fail("INVALID_PARAM", "默认方案不存在")
	}
	if f.DefaultPresetID == "" && len(f.Presets) > 0 {
		f.DefaultPresetID = f.Presets[0].ID
	}
	if len(f.Params) == 0 {
		f.Presets = []Preset{}
		f.DefaultPresetID = ""
	}

	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(data) > maxMeta {
		return fail("SIZE_LIMIT", fmt.Sprintf("元数据不能超过 %d 字节", maxMeta))
	}
	return nil
}

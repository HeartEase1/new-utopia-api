package system_setting

import (
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// ThemeDefaultPresetOptionKey selects the color preset shown to every visitor
// who has not picked one. Preset ids are defined by the frontend, which falls
// back to its built-in default for an id it does not know.
const ThemeDefaultPresetOptionKey = "theme_setting.default_preset"

type ThemeSetting struct {
	DefaultPreset string `json:"default_preset"`
}

var themeSetting = ThemeSetting{DefaultPreset: "default"}

var themePresetIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func init() {
	config.GlobalConfig.Register("theme_setting", &themeSetting)
}

func GetThemeSetting() *ThemeSetting {
	return &themeSetting
}

func ValidateThemeDefaultPreset(value string) error {
	if len(value) > 40 || !themePresetIDPattern.MatchString(value) {
		return common.NewMessage("The default theme must be a valid theme preset")
	}
	return nil
}

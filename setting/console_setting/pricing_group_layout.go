package console_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// An empty title represents a standalone group. Named entries are drawers.
type PricingGroupLayoutItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Groups      []string `json:"groups"`
	DefaultOpen bool     `json:"default_open"`
}

func ParsePricingGroupLayout(raw string) ([]PricingGroupLayoutItem, error) {
	items := []PricingGroupLayoutItem{}
	if strings.TrimSpace(raw) == "" {
		return items, nil
	}
	invalid := func() ([]PricingGroupLayoutItem, error) {
		return nil, common.NewMessage("Invalid group arrangement. Use unique entries and groups, with a name for each drawer.")
	}
	if len(raw) > 131072 || common.UnmarshalJsonStr(raw, &items) != nil || items == nil || len(items) > 1000 {
		return invalid()
	}
	ids := make(map[string]bool)
	groups := make(map[string]bool)
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" || len(item.ID) > 512 || ids[item.ID] || len([]rune(item.Title)) > 80 || (item.Title != "" && strings.TrimSpace(item.Title) == "") || (item.Title == "" && len(item.Groups) != 1) || item.Groups == nil {
			return invalid()
		}
		ids[item.ID] = true
		for _, group := range item.Groups {
			if strings.TrimSpace(group) == "" || len(group) > 256 || groups[group] {
				return invalid()
			}
			groups[group] = true
		}
	}
	if len(groups) > 1000 {
		return invalid()
	}
	return items, nil
}

// Never disclose hidden groups or titles of drawers containing only hidden groups.
// Visibility remains governed by the existing user usable-group rules.
func VisiblePricingGroupLayout(raw string, usable map[string]string) []PricingGroupLayoutItem {
	items, err := ParsePricingGroupLayout(raw)
	visible := []PricingGroupLayoutItem{}
	if err != nil {
		return visible
	}
	for _, item := range items {
		groups := []string{}
		for _, group := range item.Groups {
			if _, ok := usable[group]; ok && group != "auto" && group != "all" {
				groups = append(groups, group)
			}
		}
		if len(groups) > 0 {
			item.Groups = groups
			// IDs are public display identifiers, independent of internal group names.
			digest := sha256.Sum256([]byte(item.ID))
			item.ID = "layout-" + hex.EncodeToString(digest[:16])
			visible = append(visible, item)
		}
	}
	return visible
}

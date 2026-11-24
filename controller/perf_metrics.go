package controller

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	recentModels := c.QueryArray("recent_model")
	if len(recentModels) > perfmetrics.MaxRecentModels {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups, recentModels...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model:         modelName,
		Group:         c.Query("group"),
		Hours:         hours,
		AllowedGroups: append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetricsAvailability(c *gin.Context) {
	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	// Like the model square, regular users only see the groups they can use.
	if c.GetInt("role") < common.RoleAdminUser {
		userGroup, err := model.GetUserGroup(c.GetInt("id"), false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		usableGroups := service.GetUserUsableGroups(userGroup)
		activeGroups = slices.DeleteFunc(activeGroups, func(group string) bool {
			_, usable := usableGroups[group]
			return !usable
		})
	}
	slices.Sort(activeGroups)
	// Same order as the model square: arranged groups first, the rest by name.
	common.OptionMapRWMutex.RLock()
	layoutRaw := common.OptionMap["console_setting.pricing_group_layout"]
	common.OptionMapRWMutex.RUnlock()
	layout, _ := console_setting.ParsePricingGroupLayout(layoutRaw)
	orderedGroups := make([]string, 0, len(activeGroups))
	for _, item := range layout {
		for _, group := range item.Groups {
			if slices.Contains(activeGroups, group) {
				orderedGroups = append(orderedGroups, group)
			}
		}
	}
	for _, group := range activeGroups {
		if !slices.Contains(orderedGroups, group) {
			orderedGroups = append(orderedGroups, group)
		}
	}
	result, err := perfmetrics.QueryAvailabilityCached(orderedGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

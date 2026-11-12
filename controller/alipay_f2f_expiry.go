package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const alipayF2FExpiryTaskType = "alipay_f2f_expiry"

type alipayF2FExpiryProgress struct {
	LastID    int `json:"last_id"`
	Processed int `json:"processed"`
}

type alipayF2FExpiryHandler struct{}

func alipayF2FExpiredOrders(ctx context.Context) *gorm.DB {
	now := time.Now().Unix()
	return model.DB.WithContext(ctx).Model(&model.TopUp{}).
		Where("payment_provider = ? AND status = ?", model.PaymentProviderAlipayF2F, common.TopUpStatusPending).
		Where("(expires_at > 0 AND expires_at <= ?) OR (expires_at = 0 AND create_time <= ?)", now, now-15*60)
}

func (alipayF2FExpiryHandler) Type() string            { return alipayF2FExpiryTaskType }
func (alipayF2FExpiryHandler) Interval() time.Duration { return time.Minute }
func (alipayF2FExpiryHandler) Enabled() bool {
	// Disabled checkout must still settle/close orders already in flight.
	if _, err := getAlipayF2F(); err != nil {
		return false
	}
	var orders []model.TopUp
	result := alipayF2FExpiredOrders(context.Background()).Select("id").Limit(1).Find(&orders)
	return result.Error == nil && len(orders) > 0
}
func (alipayF2FExpiryHandler) NewPayload() any {
	var progress alipayF2FExpiryProgress
	if previous, err := model.GetLatestSystemTask(alipayF2FExpiryTaskType); err == nil && previous != nil {
		_ = common.UnmarshalJsonStr(previous.Result, &progress)
	}
	progress.Processed = 0
	return progress
}
func (alipayF2FExpiryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var progress alipayF2FExpiryProgress
	if err := task.DecodePayload(&progress); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	result, err := reconcileAlipayF2FExpiredOrders(ctx, progress.LastID)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, result, err)
}

// Both background expiry and merchant-change cleanup use a bounded, paced
// pass. The persisted task cursor prevents one bad old order starving others.
func reconcileAlipayF2FExpiredOrders(ctx context.Context, afterID int) (alipayF2FExpiryProgress, error) {
	progress := alipayF2FExpiryProgress{LastID: afterID}
	var orders []model.TopUp
	if err := alipayF2FExpiredOrders(ctx).Where("id > ?", afterID).Order("id").Limit(20).Find(&orders).Error; err != nil {
		return progress, err
	}
	if len(orders) == 0 {
		progress.LastID = 0
		return progress, nil
	}
	client, err := getAlipayF2F()
	if err != nil {
		return progress, err
	}
	for index := range orders {
		if index > 0 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return progress, ctx.Err()
			case <-timer.C:
			}
		}
		progress.LastID = orders[index].Id
		if _, err := closeAlipayF2FOrder(ctx, client, &orders[index], ""); err != nil {
			// Stop on a gateway failure instead of bursting retries. The next
			// scheduled pass continues, wrapping to retry this order later.
			return progress, err
		}
		progress.Processed++
	}
	return progress, nil
}

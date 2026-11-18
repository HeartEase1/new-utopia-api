import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import * as subscriptions from '@/features/subscriptions/api'

import { SubscriptionPlansCard } from '../components/subscription-plans-card'

function renderCard(props: {
  enabled?: boolean
  available?: (available: boolean) => void
  active?: boolean
}) {
  const subscription = {
    id: 1,
    user_id: 1,
    plan_id: 1,
    status: 'active',
    start_time: 1,
    end_time: Math.floor(Date.now() / 1000) + 86400,
    amount_total: 100,
    amount_used: 50,
  }
  vi.spyOn(subscriptions, 'getPublicPlans').mockResolvedValue({
    success: true,
    data: [],
  })
  vi.spyOn(subscriptions, 'getSelfSubscriptionFull').mockResolvedValue({
    success: true,
    data: {
      billing_preference: 'wallet_first',
      subscriptions: props.active ? [{ subscription }] : [],
      all_subscriptions: props.active
        ? [{ subscription }]
        : [
            {
              subscription: {
                ...subscription,
                status: 'expired',
                end_time: 2,
              },
            },
          ],
    },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SubscriptionPlansCard
        topupInfo={null}
        enabled={props.enabled}
        onAvailabilityChange={props.available}
      />
    </QueryClientProvider>
  )
}

describe('SubscriptionPlansCard visibility', () => {
  it('hides plans when only expired subscriptions remain', async () => {
    const available = vi.fn<(available: boolean) => void>()
    renderCard({ available })
    await waitFor(() => expect(available).toHaveBeenLastCalledWith(false))
    expect(screen.queryByText('Subscription Plans')).not.toBeInTheDocument()
  })

  it('keeps the panel for a user with an active plan even after all plans are delisted', async () => {
    renderCard({ active: true })
    expect(await screen.findByText('Subscription Plans')).toBeVisible()
  })

  it('hides the panel entirely when the administrator turns it off', async () => {
    const available = vi.fn<(available: boolean) => void>()
    renderCard({ enabled: false, available })
    await waitFor(() => expect(available).toHaveBeenLastCalledWith(false))
    expect(screen.queryByText('Subscription Plans')).not.toBeInTheDocument()
  })
})

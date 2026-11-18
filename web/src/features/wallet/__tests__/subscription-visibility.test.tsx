import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import * as subscriptions from '@/features/subscriptions/api'

import { SubscriptionPlansCard } from '../components/subscription-plans-card'

it('hides plans when only expired subscriptions remain', async () => {
  vi.spyOn(subscriptions, 'getPublicPlans').mockResolvedValue({
    success: true,
    data: [],
  })
  vi.spyOn(subscriptions, 'getSelfSubscriptionFull').mockResolvedValue({
    success: true,
    data: {
      billing_preference: 'wallet_first',
      subscriptions: [],
      all_subscriptions: [
        {
          subscription: {
            id: 1,
            user_id: 1,
            plan_id: 1,
            status: 'expired',
            start_time: 1,
            end_time: 2,
            amount_total: 100,
            amount_used: 50,
          },
        },
      ],
    },
  })
  const available = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SubscriptionPlansCard
        topupInfo={null}
        onAvailabilityChange={available}
      />
    </QueryClientProvider>
  )
  await waitFor(() => expect(available).toHaveBeenLastCalledWith(false))
  expect(screen.queryByText('Subscription Plans')).not.toBeInTheDocument()
})

it('keeps the subscription panel for a user with an active plan even after all plans are delisted', async () => {
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
      billing_preference: 'subscription_first',
      subscriptions: [{ subscription }],
      all_subscriptions: [{ subscription }],
    },
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <SubscriptionPlansCard topupInfo={null} />
    </QueryClientProvider>
  )
  expect(await screen.findByText('Subscription Plans')).toBeVisible()
})

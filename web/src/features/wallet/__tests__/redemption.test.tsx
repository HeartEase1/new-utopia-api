import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'

import { RedemptionCard } from '../components/redemption-card'
import type { TopupInfo } from '../types'

const topupInfo: TopupInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  pay_methods: [],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [],
  discount: {},
  redemption_purchase: JSON.stringify({
    name: 'Utopia Store',
    url: 'https://store.example/codes',
    fee: '2%',
    payment_methods: 'Alipay / WeChat Pay',
  }),
}

it('shows the configured store details and lets the user redeem with Enter', () => {
  const redeem = vi.fn()
  function WalletRedemption() {
    const [code, setCode] = useState('')
    return (
      <RedemptionCard
        topupInfo={topupInfo}
        code={code}
        onCodeChange={setCode}
        onRedeem={() => redeem(code)}
        redeeming={false}
      />
    )
  }
  render(<WalletRedemption />)
  expect(screen.getByRole('link', { name: 'Utopia Store' })).toHaveAttribute(
    'href',
    'https://store.example/codes'
  )
  expect(screen.getByText('2%')).toBeVisible()
  expect(screen.getByText('Alipay / WeChat Pay')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Redeem' })).toBeDisabled()
  fireEvent.change(screen.getByLabelText('Have a Code?'), {
    target: { value: 'test-code' },
  })
  fireEvent.submit(screen.getByRole('form', { name: 'Redeem' }))
  expect(redeem).toHaveBeenCalledExactlyOnceWith('test-code')
})

it.each([
  'javascript:alert(1)',
  'https://user:pass@store.example',
  '//store.example',
])('does not render unsafe purchase link %s', (url) => {
  render(
    <RedemptionCard
      topupInfo={{ ...topupInfo, redemption_purchase: JSON.stringify({ url }) }}
      code=''
      onCodeChange={vi.fn()}
      onRedeem={vi.fn()}
      redeeming={false}
    />
  )
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  expect(screen.getByLabelText('Have a Code?')).toBeVisible()
})

it('keeps legacy links until new settings are saved and hides a cleared link', () => {
  const props = {
    code: '',
    onCodeChange: vi.fn(),
    onRedeem: vi.fn(),
    redeeming: false,
  }
  const { rerender } = render(
    <RedemptionCard
      {...props}
      topupInfo={{
        ...topupInfo,
        redemption_purchase: '',
        topup_link: 'https://legacy.example',
      }}
    />
  )
  expect(screen.getByRole('link')).toHaveAttribute(
    'href',
    'https://legacy.example'
  )
  rerender(
    <RedemptionCard
      {...props}
      topupInfo={{
        ...topupInfo,
        redemption_purchase: '{"url":""}',
        topup_link: 'https://legacy.example',
      }}
    />
  )
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})

it('does not allow another redemption while processing and respects redemption availability', () => {
  const redeem = vi.fn()
  const props = {
    code: 'test-code',
    onCodeChange: vi.fn(),
    onRedeem: redeem,
    redeeming: true,
  }
  const { rerender } = render(
    <RedemptionCard {...props} topupInfo={topupInfo} />
  )
  fireEvent.submit(screen.getByRole('form', { name: 'Redeem' }))
  expect(redeem).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Redeem' })).toBeDisabled()
  rerender(
    <RedemptionCard
      {...props}
      topupInfo={{ ...topupInfo, enable_redemption: false }}
    />
  )
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  expect(screen.queryByLabelText('Have a Code?')).not.toBeInTheDocument()
})

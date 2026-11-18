import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { WalletAnnouncement } from '../components/wallet-announcement'

let animation: FrameRequestCallback | undefined
let reducedMotion = false
let contentHeight = 300

beforeEach(() => {
  reducedMotion = false
  contentHeight = 300
  animation = undefined
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    matches: query.includes('prefers-reduced-motion') && reducedMotion,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: () => true,
  }))
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(
    () => contentHeight
  )
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(96)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    animation = callback
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    animation = undefined
  })
})

afterEach(() => vi.unstubAllGlobals())

it('scrolls overflowing text, pauses on hover, and preserves manual scrolling', () => {
  render(<WalletAnnouncement content={'Notice\n'.repeat(15)} />)
  const region = screen.getByRole('region', { name: 'Wallet announcement' })
  let scrollTop = 0
  Object.defineProperty(region, 'scrollTop', {
    get: () => scrollTop,
    set: (value: number) => {
      scrollTop = Math.round(value)
    },
  })
  const start = performance.now() + 2100
  act(() => {
    animation?.(start)
    animation?.(start + 16)
    animation?.(start + 32)
    animation?.(start + 48)
  })
  expect(region.scrollTop).toBeGreaterThan(0)
  const scrollArea = region.closest('[data-slot="scroll-area"]')
  expect(scrollArea).not.toBeNull()
  if (!scrollArea) throw new Error('Missing announcement scroll area')
  fireEvent.mouseEnter(scrollArea)
  expect(animation).toBeUndefined()
  region.scrollTop = 100
  fireEvent.scroll(region)
  expect(region.scrollTop).toBe(100)
  fireEvent.mouseLeave(scrollArea)
  expect(animation).toBeTypeOf('function')
  fireEvent.focus(region)
  expect(animation).toBeUndefined()
  fireEvent.blur(region)
  expect(animation).toBeTypeOf('function')
})

it('allows pausing autoplay and keeps touch scrolling paused until resumed', () => {
  render(<WalletAnnouncement content={'Notice\n'.repeat(15)} />)
  fireEvent.click(screen.getByRole('button', { name: 'Pause scrolling' }))
  expect(animation).toBeUndefined()
  fireEvent.click(screen.getByRole('button', { name: 'Resume scrolling' }))
  expect(animation).toBeTypeOf('function')
  fireEvent.touchStart(
    screen.getByRole('region', { name: 'Wallet announcement' })
  )
  expect(animation).toBeUndefined()
  expect(screen.getByRole('button', { name: 'Resume scrolling' })).toBeVisible()
})

it('does not animate short content or display an empty announcement', () => {
  contentHeight = 48
  const { rerender } = render(<WalletAnnouncement content='Short notice' />)
  expect(animation).toBeUndefined()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  rerender(<WalletAnnouncement content=' ' />)
  expect(screen.queryByRole('region')).not.toBeInTheDocument()
})

it('respects reduced motion and still exposes the full text for manual scrolling', () => {
  reducedMotion = true
  render(<WalletAnnouncement content={'Notice\n'.repeat(15)} />)
  expect(animation).toBeUndefined()
  expect(
    screen.getByRole('region', { name: 'Wallet announcement' })
  ).toHaveAttribute('tabindex', '0')
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

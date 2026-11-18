import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { WalletAnnouncement } from '../components/wallet-announcement'

let animation: FrameRequestCallback | undefined
let reducedMotion = false
let contentWidth = 900

beforeEach(() => {
  reducedMotion = false
  contentWidth = 900
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
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(
    () => contentWidth
  )
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(320)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    animation = callback
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    animation = undefined
  })
})

afterEach(() => vi.unstubAllGlobals())

it('scrolls overflowing text sideways, pauses on hover, and honours manual scrolling', () => {
  render(<WalletAnnouncement content='A very long wallet notice' />)
  const region = screen.getByRole('region', { name: 'Wallet announcement' })
  let scrollLeft = 0
  Object.defineProperty(region, 'scrollLeft', {
    get: () => scrollLeft,
    set: (value: number) => {
      scrollLeft = Math.round(value)
    },
  })
  const start = performance.now() + 5000
  act(() => {
    // The first frames sit on the pause held at the start edge.
    animation?.(start)
    animation?.(start + 1500)
    animation?.(start + 3000)
    animation?.(start + 4500)
  })
  expect(region.scrollLeft).toBeGreaterThan(0)
  fireEvent.mouseEnter(region)
  expect(animation).toBeUndefined()
  region.scrollLeft = 100
  fireEvent.scroll(region)
  expect(region.scrollLeft).toBe(100)
  fireEvent.mouseLeave(region)
  expect(animation).toBeTypeOf('function')
  fireEvent.focus(region)
  expect(animation).toBeUndefined()
  fireEvent.blur(region)
  expect(animation).toBeTypeOf('function')
})

it('drives the region with the wheel while overflowing', () => {
  render(<WalletAnnouncement content='A very long wallet notice' />)
  const region = screen.getByRole('region', { name: 'Wallet announcement' })
  const scrollBy = vi.fn()
  Object.defineProperty(region, 'scrollBy', { value: scrollBy })
  fireEvent.wheel(region, { deltaY: 120 })
  expect(scrollBy).toHaveBeenCalledWith({ left: 120 })
})

it('allows pausing autoplay and keeps touch scrolling paused until resumed', () => {
  render(<WalletAnnouncement content='A very long wallet notice' />)
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

it('collapses line breaks into one line', () => {
  render(<WalletAnnouncement content={'First line\nSecond line'} />)
  expect(
    screen.getByRole('region', { name: 'Wallet announcement' })
  ).toHaveTextContent('First line · Second line')
})

it('does not animate short content or display an empty announcement', () => {
  contentWidth = 200
  const { rerender } = render(<WalletAnnouncement content='Short notice' />)
  expect(animation).toBeUndefined()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  rerender(<WalletAnnouncement content=' ' />)
  expect(screen.queryByRole('region')).not.toBeInTheDocument()
})

it('respects reduced motion and still exposes the full text for manual scrolling', () => {
  reducedMotion = true
  render(<WalletAnnouncement content='A very long wallet notice' />)
  expect(animation).toBeUndefined()
  expect(
    screen.getByRole('region', { name: 'Wallet announcement' })
  ).toHaveAttribute('tabindex', '0')
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})

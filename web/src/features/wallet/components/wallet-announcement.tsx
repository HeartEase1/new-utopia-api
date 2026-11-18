import { Megaphone, Pause, Play } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { cn } from '@/lib/utils'

const SPEED_PX_PER_SECOND = 34
const EDGE_HOLD_MS = 1400

/**
 * One-line wallet notice. Text that does not fit scrolls on its own, ping-pong
 * style, and stops while the pointer or keyboard focus is on it so it can be
 * scrolled by hand.
 */
export function WalletAnnouncement(props: { content: string }) {
  const { t } = useTranslation()
  const viewportRef = useRef<HTMLDivElement>(null)
  const contentRef = useRef<HTMLSpanElement>(null)
  const [overflow, setOverflow] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [reducedMotion, setReducedMotion] = useState(false)
  const maxScroll = useRef(0)

  const text = props.content.replaceAll(/\s*\n\s*/g, ' · ').trim()

  useEffect(() => {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)')
    const update = () => setReducedMotion(media.matches)
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])

  useEffect(() => {
    const viewport = viewportRef.current
    const content = contentRef.current
    if (!viewport || !content) return
    viewport.scrollLeft = 0
    const measure = () => {
      maxScroll.current = Math.max(
        0,
        viewport.scrollWidth - viewport.clientWidth
      )
      setOverflow(maxScroll.current > 1)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(viewport)
    observer.observe(content)
    return () => observer.disconnect()
  }, [text])

  useEffect(() => {
    const viewport = viewportRef.current
    if (
      !viewport ||
      !overflow ||
      paused ||
      hovered ||
      focused ||
      reducedMotion
    ) {
      return
    }
    let frame = 0
    let previous = 0
    let position = viewport.scrollLeft
    let direction = 1
    let holdUntil = performance.now() + EDGE_HOLD_MS
    const animate = (now: number) => {
      const elapsed = previous ? Math.min(now - previous, 100) : 0
      previous = now
      if (!document.hidden && now >= holdUntil) {
        position += (elapsed * SPEED_PX_PER_SECOND * direction) / 1000
        if (position >= maxScroll.current) {
          position = maxScroll.current
          direction = -1
          holdUntil = now + EDGE_HOLD_MS
        } else if (position <= 0) {
          position = 0
          direction = 1
          holdUntil = now + EDGE_HOLD_MS
        }
        viewport.scrollLeft = position
      }
      frame = requestAnimationFrame(animate)
    }
    frame = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(frame)
  }, [overflow, paused, hovered, focused, reducedMotion, text])

  if (!text) return null

  const showControls = overflow && !reducedMotion

  return (
    <div
      className='from-primary/8 via-muted/40 to-muted/40 relative flex items-center gap-2.5 overflow-hidden rounded-xl border bg-linear-to-r py-2 pr-2 pl-3'
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <IconBadge size='xs' tone='primary' className='z-10'>
        <Megaphone />
      </IconBadge>
      <div
        ref={viewportRef}
        role='region'
        aria-label={t('Wallet announcement')}
        tabIndex={0}
        className={cn(
          // No scroll-smooth here: it would animate every frame of the autoplay loop.
          'no-scrollbar min-w-0 flex-1 overflow-x-auto rounded-md',
          'focus-visible:ring-ring/50 outline-none focus-visible:ring-[3px]'
        )}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onTouchStart={() => setPaused(true)}
        onWheel={(event) => {
          if (!overflow) return
          const delta =
            Math.abs(event.deltaX) > Math.abs(event.deltaY)
              ? event.deltaX
              : event.deltaY
          if (delta === 0) return
          event.preventDefault()
          viewportRef.current?.scrollBy({ left: delta })
        }}
      >
        <span
          ref={contentRef}
          className='inline-block pr-6 text-sm leading-6 whitespace-nowrap'
        >
          {text}
        </span>
      </div>
      {showControls && (
        <Button
          type='button'
          variant='ghost'
          size='icon-xs'
          className='shrink-0'
          aria-label={paused ? t('Resume scrolling') : t('Pause scrolling')}
          onClick={() => setPaused(!paused)}
        >
          {paused ? <Play aria-hidden='true' /> : <Pause aria-hidden='true' />}
        </Button>
      )}
    </div>
  )
}

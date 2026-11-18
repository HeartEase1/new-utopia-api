import { Megaphone, Pause, Play } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'

export function WalletAnnouncement(props: { content: string }) {
  const { t } = useTranslation()
  const viewportRef = useRef<HTMLDivElement>(null)
  const contentRef = useRef<HTMLDivElement>(null)
  const [overflow, setOverflow] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [reducedMotion, setReducedMotion] = useState(false)
  const maxScroll = useRef(0)

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
    viewport.scrollTop = 0
    const measure = () => {
      maxScroll.current = Math.max(
        0,
        viewport.scrollHeight - viewport.clientHeight
      )
      setOverflow(maxScroll.current > 1)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(viewport)
    observer.observe(content)
    return () => observer.disconnect()
  }, [props.content])

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
    let position = viewport.scrollTop
    let holdUntil = performance.now() + 2000
    let resetAtEnd = false
    const animate = (now: number) => {
      const elapsed = previous ? Math.min(now - previous, 100) : 0
      previous = now
      if (!document.hidden && now >= holdUntil) {
        if (resetAtEnd) {
          position = 0
          viewport.scrollTop = 0
          resetAtEnd = false
          holdUntil = now + 2000
        } else {
          // Keep subpixel progress even when the browser rounds scrollTop.
          position += elapsed * 0.025
          viewport.scrollTop = position
          if (viewport.scrollTop >= maxScroll.current - 1) {
            resetAtEnd = true
            holdUntil = now + 2000
          }
        }
      }
      frame = requestAnimationFrame(animate)
    }
    frame = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(frame)
  }, [overflow, paused, hovered, focused, reducedMotion, props.content])

  if (!props.content.trim()) return null

  return (
    <div
      className='bg-muted/40 flex min-w-0 items-start gap-2 rounded-lg border p-3'
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <Megaphone
        className='text-primary mt-0.5 size-4 shrink-0'
        aria-hidden='true'
      />
      <ScrollArea
        className='min-w-0 flex-1'
        viewportProps={{
          ref: viewportRef,
          className: 'max-h-24',
          role: 'region',
          'aria-label': t('Wallet announcement'),
          tabIndex: 0,
          onFocus: () => setFocused(true),
          onBlur: () => setFocused(false),
          onTouchStart: () => setPaused(true),
        }}
      >
        <div
          ref={contentRef}
          className='pr-3 text-sm leading-6 break-words whitespace-pre-wrap'
        >
          {props.content}
        </div>
      </ScrollArea>
      {overflow && !reducedMotion && (
        <Button
          type='button'
          variant='ghost'
          size='icon-xs'
          aria-label={paused ? t('Resume scrolling') : t('Pause scrolling')}
          onClick={() => setPaused(!paused)}
        >
          {paused ? <Play aria-hidden='true' /> : <Pause aria-hidden='true' />}
        </Button>
      )}
    </div>
  )
}

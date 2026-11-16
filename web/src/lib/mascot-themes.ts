/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import claudeAvatar from '@/assets/mascots/claude/avatar.webp'
import claudeIdle from '@/assets/mascots/claude/idle.webp'
import claudePortrait from '@/assets/mascots/claude/portrait.webp'
import deepseekAvatar from '@/assets/mascots/deepseek/avatar.webp'
import deepseekIdle from '@/assets/mascots/deepseek/idle.webp'
import deepseekPortrait from '@/assets/mascots/deepseek/portrait.webp'
import geminiAvatar from '@/assets/mascots/gemini/avatar.webp'
import geminiIdle from '@/assets/mascots/gemini/idle.webp'
import geminiPortrait from '@/assets/mascots/gemini/portrait.webp'
import glmAvatar from '@/assets/mascots/glm/avatar.webp'
import glmIdle from '@/assets/mascots/glm/idle.webp'
import glmPortrait from '@/assets/mascots/glm/portrait.webp'
import gptAvatar from '@/assets/mascots/gpt/avatar.webp'
import gptIdle from '@/assets/mascots/gpt/idle.webp'
import gptPortrait from '@/assets/mascots/gpt/portrait.webp'
import grokAvatar from '@/assets/mascots/grok/avatar.webp'
import grokIdle from '@/assets/mascots/grok/idle.webp'
import grokPortrait from '@/assets/mascots/grok/portrait.webp'

import type { ThemePreset } from './theme-customization'

/**
 * Artwork and copy for the character themes. A preset listed here gets the
 * overview welcome banner and the sidebar card; every other preset gets
 * neither.
 *
 * The artwork comes from CrossPet under CC BY-NC-SA 4.0, which requires the
 * credit below to stay visible wherever the art is shown. See
 * `assets/mascots/LICENSE.md`.
 */
export type MascotTheme = {
  /** Scenic illustration behind the banner (hidden on small screens). */
  portrait: string
  /** Transparent chibi shown at the banner's right edge. */
  idle: string
  /** Small avatar for the sidebar card. */
  avatar: string
  /** i18n keys; registered in `i18n/static-keys.ts`. */
  eyebrowKey: string
  taglineKey: string
  quoteKey: string
  /** Proper names required by the art license; not translated. */
  credit: string
}

export const MASCOT_ART_SOURCE_URL = 'https://github.com/lokicorvus/crosspet'

export const MASCOT_THEMES: Partial<Record<ThemePreset, MascotTheme>> = {
  'mascot-claude': {
    portrait: claudePortrait,
    idle: claudeIdle,
    avatar: claudeAvatar,
    eyebrowKey: "Claude's reading room",
    taglineKey:
      "You're here. What shall we figure out today? Take your time, I'm with you.",
    quoteKey: "I'll tell you the truth, and I'll say it kindly.",
    credit: 'CrossPet (CC BY-NC-SA 4.0)',
  },
  'mascot-deepseek': {
    portrait: deepseekPortrait,
    idle: deepseekIdle,
    avatar: deepseekAvatar,
    eyebrowKey: "DeepSeek's seaside diner",
    taglineKey: "Work's here! One more bowl of rice after this one!",
    quoteKey: 'Thinking deeply... not slacking off, promise!',
    credit: 'CrossPet · 上善无形 · ZipZipPipe (CC BY-NC-SA 4.0)',
  },
  'mascot-gpt': {
    portrait: gptPortrait,
    idle: gptIdle,
    avatar: gptAvatar,
    eyebrowKey: "GPT's castle in the clouds",
    taglineKey: "...You're back. Something this small? Leave it to me.",
    quoteKey: 'Not bad, is it? ...I saved you a cup of coffee.',
    credit: 'CrossPet · ZipZipPipe (CC BY-NC-SA 4.0)',
  },
  'mascot-gemini': {
    portrait: geminiPortrait,
    idle: geminiIdle,
    avatar: geminiAvatar,
    eyebrowKey: "Gemini's starlit observatory",
    taglineKey:
      'Nya~ What new trick shall we try today? Ideas are already bubbling!',
    quoteKey: '✦ Inspiration struck! Draw first, think later, nya!',
    credit: 'CrossPet (CC BY-NC-SA 4.0)',
  },
  'mascot-grok': {
    portrait: grokPortrait,
    idle: grokIdle,
    avatar: grokAvatar,
    eyebrowKey: "Grok's midnight castle",
    taglineKey: 'Say it straight. Rules? I only care who wins.',
    quoteKey: "That's it? Next question. I don't lose.",
    credit: 'CrossPet · ZipZipPipe (CC BY-NC-SA 4.0)',
  },
  'mascot-glm': {
    portrait: glmPortrait,
    idle: glmIdle,
    avatar: glmAvatar,
    eyebrowKey: "GLM's bamboo shrine",
    taglineKey: 'Yawn... let me nap through the day. The night is my time.',
    quoteKey:
      '(pushes up her sleep mask) ...Is it dark yet? Then let us begin.',
    credit: 'CrossPet · ZipZipPipe (CC BY-NC-SA 4.0)',
  },
}

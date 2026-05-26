/** Presentation SSE 事件（Go ChatProxy 输出） */
export type PresentationEvent = {
  type: string
  content?: string
  skeleton?: string
  meta?: Record<string, unknown>
}

export type ToolActivity = {
  id: string
  content: string
  done: boolean
  ok?: boolean
}

export type AgentStreamState = {
  streamingContent: string
  statusText: string
  phaseSkeleton: string
  toolActivities: ToolActivity[]
  streamError: boolean
  done: boolean
}

export function createAgentStreamState(): AgentStreamState {
  return {
    streamingContent: '',
    statusText: '思考中…',
    phaseSkeleton: '',
    toolActivities: [],
    streamError: false,
    done: false,
  }
}

function toolCallId(ev: PresentationEvent, fallback: number): string {
  const id = ev.meta?.tool_call_id
  return typeof id === 'string' && id ? id : 'tc_' + fallback
}

export function reducePresentationEvent(
  state: AgentStreamState,
  event: PresentationEvent,
): AgentStreamState {
  const next: AgentStreamState = {
    ...state,
    toolActivities: [...state.toolActivities],
  }

  switch (event.type) {
    case 'thinking':
      if (event.content) next.statusText = event.content
      break

    case 'phase':
      next.phaseSkeleton = event.skeleton || 'planning'
      if (event.content) next.statusText = event.content
      break

    case 'tool_start': {
      const id = toolCallId(event, next.toolActivities.length + 1)
      next.toolActivities.push({
        id,
        content: event.content || '正在处理…',
        done: false,
      })
      if (event.content) next.statusText = event.content
      break
    }

    case 'tool_end': {
      const id = toolCallId(event, next.toolActivities.length)
      let idx = next.toolActivities.findIndex((t) => t.id === id && !t.done)
      if (idx < 0) {
        for (let i = next.toolActivities.length - 1; i >= 0; i--) {
          if (!next.toolActivities[i].done) {
            idx = i
            break
          }
        }
      }
      if (idx >= 0) {
        const row = { ...next.toolActivities[idx], done: true }
        if (event.content) row.content = event.content
        if (typeof event.meta?.ok === 'boolean') row.ok = event.meta.ok as boolean
        next.toolActivities[idx] = row
      } else if (event.content) {
        next.toolActivities.push({
          id,
          content: event.content,
          done: true,
          ok: event.meta?.ok as boolean | undefined,
        })
      }
      break
    }

    case 'text_chunk':
      if (event.content) {
        next.streamingContent += event.content
        next.phaseSkeleton = ''
      }
      break

    case 'done':
      next.done = true
      next.statusText = '就绪'
      next.phaseSkeleton = ''
      break

    case 'error':
      next.streamError = true
      next.done = true
      next.statusText = '出错'
      next.phaseSkeleton = ''
      break

    default:
      break
  }

  return next
}

export function reduceLegacyToken(state: AgentStreamState, content: string): AgentStreamState {
  return {
    ...state,
    streamingContent: state.streamingContent + content,
    phaseSkeleton: '',
  }
}

export function applyPresentationEvent(
  state: AgentStreamState,
  raw: Record<string, unknown>,
): AgentStreamState {
  if (raw.type === 'token' && typeof raw.content === 'string') {
    return reduceLegacyToken(state, raw.content)
  }
  const t = raw.type
  if (typeof t !== 'string') return state
  const known = new Set([
    'thinking',
    'phase',
    'tool_start',
    'tool_end',
    'text_chunk',
    'done',
    'error',
  ])
  if (!known.has(t)) return state
  return reducePresentationEvent(state, raw as PresentationEvent)
}

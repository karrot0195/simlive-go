import { client } from './client'

export interface AuthField {
  name: string
  type: string
  label: string
  required?: boolean
  default?: string
  options?: any
  optionValueField?: string
  optionLabelField?: string
  itemValue?: string
  itemTitle?: string
  valueField?: string
  labelField?: string
}

export interface PreCall {
  url: string
  method: string
  headers?: Record<string, string>
  body?: any
  response_mapping?: Record<string, string>
}

export interface AuthStep {
  title: string
  fields?: AuthField[]
  api: string
  method: string
  response_field: string
  headers?: Record<string, string>
  body?: any
  preCalls?: PreCall[]
  validation?: any
  style?: any
}

export interface AuthVerify {
  url: string
  method?: string
  headers?: Record<string, string>
  body?: any
  response_ok?: string
  clear_on_error?: boolean
}

export interface AuthConfig {
  steps: AuthStep[]
  verify?: AuthVerify
  style?: any
}

export type Ctx = Record<string, any>

export function resolvePlaceholders(value: any, ctx: Ctx): any {
  if (typeof value === 'string') {
    return value.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key) => {
      const v = key.split('.').reduce((o: any, k: string) => (o == null ? undefined : o[k]), ctx)
      return v == null ? '' : String(v)
    })
  }
  if (Array.isArray(value)) return value.map((v) => resolvePlaceholders(v, ctx))
  if (value && typeof value === 'object') {
    const out: Record<string, any> = {}
    for (const k of Object.keys(value)) out[k] = resolvePlaceholders(value[k], ctx)
    return out
  }
  return value
}

export async function fetchAuthConfig(): Promise<AuthConfig> {
  try {
    const res = await client.get('/auth/config')
    if (res?.data?.steps) return res.data as AuthConfig
  } catch (e) {
    console.warn('auth config API unavailable, using fallback', e)
  }
  try {
    const res = await fetch('/auth-config.json')
    if (res.ok) {
      const data = await res.json()
      if (data?.steps) return data as AuthConfig
    }
  } catch (e) {
    console.warn('bundled auth config unavailable', e)
  }
  return { steps: [] }
}

export async function runPreCalls(preCalls: PreCall[] | undefined, ctx: Ctx): Promise<void> {
  if (!preCalls) return
  for (const pc of preCalls) {
    const url = resolvePlaceholders(pc.url, ctx)
    const headers = resolvePlaceholders(pc.headers || {}, ctx)
    const body = resolvePlaceholders(pc.body, ctx)
    const res = await fetch(url, {
      method: pc.method,
      headers: { 'Content-Type': 'application/json', ...headers },
      body: pc.method !== 'GET' ? JSON.stringify(body ?? {}) : undefined,
    })
    const data = await res.json().catch(() => ({}))
    if (pc.response_mapping) {
      for (const [k, path] of Object.entries(pc.response_mapping)) {
        if (!path) {
          ctx[k] = data
          continue
        }
        if (path === '$') {
          ctx[k] = data
          continue
        }
        const p = path.replace(/^\$\./, '')
        const v = p.split('.').reduce((o: any, key: string) => (o == null ? undefined : o[key]), data)
        ctx[k] = v
      }
    }
    ctx.response = data
  }
}

export function resolveOptions(options: any, ctx: Ctx): Array<any> {
  if (Array.isArray(options)) return options
  if (typeof options === 'string') {
    if (options.startsWith('${') && options.endsWith('}')) {
      const key = options.slice(2, -1).trim()
      const v = key.split('.').reduce((o: any, k: string) => (o == null ? undefined : o[k]), ctx)
      if (Array.isArray(v)) return v
      return []
    }
    const resolved = resolvePlaceholders(options, ctx)
    return Array.isArray(resolved) ? resolved : []
  }
  return []
}

export function getOptionValue(item: any, field?: any): string {
  if (item == null) return ''
  if (typeof item === 'string') return item
  if (typeof item === 'object') {
    const key = field?.optionValueField || field?.itemValue || field?.valueField
    if (key && item[key] != null) return String(item[key])
    const v = item.id ?? item.value ?? item.accountId ?? item.name
    return v == null ? JSON.stringify(item) : String(v)
  }
  return ''
}

export function getOptionLabel(item: any, field?: any): string {
  if (item == null) return ''
  if (typeof item === 'string') return item
  if (typeof item === 'object') {
    const key = field?.optionLabelField || field?.itemTitle || field?.labelField
    if (key && item[key] != null) return String(item[key])
    const v = item.name ?? item.label ?? item.title ?? item.email ?? getOptionValue(item, field)
    return v == null ? '' : String(v)
  }
  return ''
}

export function getByPath(obj: any, path: string): any {
  if (!obj || !path) return undefined
  return path.split('.').reduce((o: any, k: string) => (o == null ? undefined : o[k]), obj)
}

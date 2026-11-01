const envs: any = {
  default: {
    restAPI: import.meta.env.VITE_REST_API || 'http://localhost:3030/api/v2',
    cdnLive: import.meta.env.VITE_CDN_LIVE || 'http://localhost:8080/live',
    api: import.meta.env.VITE_API || 'http://localhost:8080/api'
  },
  test: {
    restAPI: import.meta.env.VITE_REST_API || 'http://localhost:3030/api/v2',
    cdnLive: import.meta.env.VITE_CDN_LIVE || 'http://localhost:8080/live',
    api: import.meta.env.VITE_API || 'http://localhost:8080/api'
  },
  production: {
    api: import.meta.env.VITE_API || '/api',
    restAPI: import.meta.env.VITE_REST_API || '/api/v2',
    cdnLive: import.meta.env.VITE_CDN_LIVE || '/live'
  },
  preprod: {
    api: import.meta.env.VITE_API || '/api',
    restAPI: import.meta.env.VITE_REST_API || '/api/v2',
    cdnLive: import.meta.env.VITE_CDN_LIVE || '/live'
  }
}
// @ts-ignore
const envName: any = process.env.NODE_ENV in envs ? process.env.NODE_ENV : 'default'
// @ts-ignore
const config = {
  ...envs.default,
  ...(envName in envs ? envs[envName] : {})
}
// @ts-ignore
const API = import.meta.env.VITE_API || config.api
const REST_API = import.meta.env.VITE_REST_API || config.restAPI
// @ts-ignore
const LIVE_API = import.meta.env.VITE_CDN_LIVE || config.cdnLive

export { API, LIVE_API, REST_API }
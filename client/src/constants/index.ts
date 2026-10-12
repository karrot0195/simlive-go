const envs: any = {
  default: {
    restAPI: 'https://sample.cloud/api/v2',
    cdnLive: 'http://localhost:8080/live',
    api: 'http://localhost:8080/api'
  },
  production: {
    api: '/api',
    restAPI: 'https://sample.cloud/api/v2',
    cdnLive: '/live'
  },
}
// @ts-ignore
const envName: any = process.env.NODE_ENV in envs ? process.env.NODE_ENV : 'default'

// @ts-ignore
const config = {
  ...envs.default,
  ...(envName in envs ? envs[envName] : {})
}

// @ts-ignore
const API = config.api
const REST_API = config.restAPI
// @ts-ignore
const LIVE_API = config.cdnLive

export { API, LIVE_API, REST_API }

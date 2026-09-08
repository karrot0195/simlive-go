const envs: any = {
  default: {
    restAPI: 'https://api2.espx.cloud/api/v2',
    cdnLive: 'http://localhost:8080/live',
    api: 'http://localhost:8080/api'
  },
  test: {
    restAPI: 'https://api2.espx.cloud/api/v2',
    cdnLive: 'http://45.77.1.159/live',
    api: 'http://45.77.1.159/api'
  },
  production: {
    api: '/api',
    restAPI: 'https://api2.espx.cloud/api/v2',
    cdnLive: '/live'
  },
  preprod: {
    api: '/api',
    restAPI: 'https://preprod-api2.espx.cloud/api/v2',
    cdnLive: 'https://cdn-518.espx.cloud/live'
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
const API = config.api
const REST_API = config.restAPI
// @ts-ignore
const LIVE_API = config.cdnLive

export { API, LIVE_API, REST_API }

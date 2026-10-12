const config : {
  restAPI: string,
  cdnLive: string,
  api: string
} = {
  restAPI: process.env.VITE_REST_API || '',
  cdnLive: process.env.VITE_REST_API || '/live',
  api: process.env.VITE_API || '/api',

}

const API = config.api
const REST_API = config.restAPI
const LIVE_API = config.cdnLive

export { API, LIVE_API, REST_API }

import { LIVE_API } from '@/constants'
import axios from 'axios'

const liveClient = axios.create({ baseURL: LIVE_API })

export { liveClient }

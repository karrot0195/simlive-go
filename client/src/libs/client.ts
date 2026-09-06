import { API, REST_API } from '@/constants'
import axios from 'axios'

const client = axios.create({
    baseURL: API, headers: {
        Authorization: `Bearer ${localStorage.getItem('token') || ''}`
    }
})
const restClient = axios.create({ baseURL: REST_API })

export { client, restClient }

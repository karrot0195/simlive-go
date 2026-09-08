import { API, REST_API } from '@/constants'
import axios from 'axios'

const client = axios.create({
    baseURL: API
})

// Re-read the bearer token on every request so a sign-in that happens
// after module load is honoured. Previously the header was captured
// once at module-eval time, which silently ignored late token writes.
client.interceptors.request.use((config) => {
    const token = localStorage.getItem('token')
    if (token) {
        config.headers.set('Authorization', `Bearer ${token}`)
    } else {
        // config.headers.delete('Authorization')
    }
    return config
})

const restClient = axios.create({ baseURL: REST_API })

export { client, restClient }

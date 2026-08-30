import { createApp } from 'vue'
import App from './App.vue'

// Tailwind CSS (loaded first so v10 component CSS can override it)
import './style.css'

// v10 default skin CSS
import '@videojs/html/video/skin.css'

createApp(App).mount('#app')

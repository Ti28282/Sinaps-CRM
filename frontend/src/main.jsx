import React from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource/onest/400.css'
import '@fontsource/onest/500.css'
import '@fontsource/onest/600.css'
import '@fontsource/onest/700.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'
import './styles.css'
import App from './App.jsx'
import { api, applyTheme } from './api.js'

// тема — до первого кадра, чтобы не мигало
api.config().then((c) => applyTheme(c.theme)).catch(() => applyTheme('dark')).finally(() => {
  createRoot(document.getElementById('root')).render(<App />)
})

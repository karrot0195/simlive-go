import { ref, watch } from 'vue'

export interface SectionSettings {
    id: string
    visible: boolean
    order: number
}

const STORAGE_KEY = 'log-section-settings'

export function useLogSectionSettings() {
    const sectionSettings = ref<Map<string, SectionSettings>>(new Map())

    const loadSettings = () => {
        const stored = localStorage.getItem(STORAGE_KEY)
        if (stored) {
            try {
                const parsed = JSON.parse(stored)
                sectionSettings.value = new Map(Object.entries(parsed))
            } catch (e) {
                console.error('Failed to load section settings:', e)
            }
        }
    }

    const saveSettings = () => {
        const obj = Object.fromEntries(sectionSettings.value)
        localStorage.setItem(STORAGE_KEY, JSON.stringify(obj))
    }

    const getSectionSettings = (contextId: string): SectionSettings => {
        if (!sectionSettings.value.has(contextId)) {
            const newSettings: SectionSettings = {
                id: contextId,
                visible: true,
                order: sectionSettings.value.size
            }
            sectionSettings.value.set(contextId, newSettings)
            saveSettings()
        }
        return sectionSettings.value.get(contextId)!
    }

    const toggleSectionVisibility = (contextId: string) => {
        const settings = getSectionSettings(contextId)
        settings.visible = !settings.visible
        sectionSettings.value.set(contextId, settings)
        saveSettings()
    }

    const updateSectionOrder = (contextId: string, newOrder: number) => {
        const settings = getSectionSettings(contextId)
        settings.order = newOrder
        sectionSettings.value.set(contextId, settings)
        saveSettings()
    }

    const reorderSections = (oldIndex: number, newIndex: number, orderedContexts: string[]) => {
        // Update all orders based on new positions
        orderedContexts.forEach((contextId, index) => {
            updateSectionOrder(contextId, index)
        })
    }

    // Load settings on initialization
    loadSettings()

    return {
        sectionSettings,
        getSectionSettings,
        toggleSectionVisibility,
        updateSectionOrder,
        reorderSections,
        loadSettings,
        saveSettings
    }
}

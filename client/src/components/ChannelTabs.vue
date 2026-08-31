<script setup lang="ts">
// Tab strip that swaps between the channel list and the HLS session info
// panel. Default tab is "channels". Active tab is local state — switching
// is instant and doesn't reload data (each child manages its own lifecycle).

import { ref } from "vue";
import ChannelListPanel from "./ChannelListPanel.vue";
import SessionInfoPanel from "./SessionInfoPanel.vue";

type TabId = "channels" | "session";

const activeTab = ref<TabId>("channels");
</script>

<template>
  <channel-tabs class="block w-full h-full overflow-hidden flex flex-col">
    <!-- Tab strip -->
    <div
      class="flex items-center gap-1 px-2 py-1.5 border-b border-slate-800 bg-slate-900/80"
      role="tablist"
      aria-label="Sidebar tabs"
    >
      <button
        type="button"
        role="tab"
        :aria-selected="activeTab === 'channels'"
        class="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-widest rounded-md transition"
        :class="
          activeTab === 'channels'
            ? 'bg-sky-500/20 text-sky-200 border border-sky-500/40'
            : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 border border-transparent'
        "
        @click="activeTab = 'channels'"
      >
        Channels
      </button>
      <button
        type="button"
        role="tab"
        :aria-selected="activeTab === 'session'"
        class="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-widest rounded-md transition"
        :class="
          activeTab === 'session'
            ? 'bg-sky-500/20 text-sky-200 border border-sky-500/40'
            : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 border border-transparent'
        "
        @click="activeTab = 'session'"
      >
        Session Info
      </button>
    </div>

    <!-- Tab content -->
    <div class="flex-1 min-h-0 overflow-y-auto">
      <ChannelListPanel v-show="activeTab === 'channels'" />
      <SessionInfoPanel v-show="activeTab === 'session'" />
    </div>
  </channel-tabs>
</template>

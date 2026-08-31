<script setup lang="ts">
// Channel browser sidebar.
//
// - Fetches local playout channels via `/api/channels` (DB-backed list).
// - Fetches the upstream ESPX fleet via `/api/channels/esp` so users can
//   register unregistered channels with one click.
// - Per-row actions: Start / Stop (with optimistic update + revert on
//   failure) and Select (pushes the channel into the shared `channelStore`,
//   which `VideoPlayer.vue` watches).
// - Auto-refresh every `REFRESH_MS` while mounted. Cancels on unmount.

import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  ApiError,
  createChannel,
  listChannels,
  listESpxChannels,
  startChannel,
  stopChannel,
  type ESpxChannelsListResp,
  type ListChannelsResp,
  type PlayoutChannel,
} from "../api/channels.ts";
import {
  channelStore,
  clearActiveChannel,
  pendingChannelFromUrl,
  selectChannel,
  setActiveChannelId,
} from "./channelStore.ts";

const REFRESH_MS = 10_000;
const REQUEST_TIMEOUT_MS = 10_000;

type SourceTab = "local" | "espx";

const sourceTab = ref<SourceTab>("local");
const loading = ref(false);
const errorMessage = ref<string | null>(null);
const toast = ref<{ kind: "success" | "error"; text: string } | null>(null);

// Track which channel ids are mid-request so we can disable the row's
// buttons and show an inline spinner.
type PendingAction = "start" | "stop" | "register" | null;
const pendingAction = ref<Record<string, PendingAction>>({});

// Track which ESPX rows are mid-registration.
const localChannels = ref<PlayoutChannel[]>([]);
const espx = ref<ESpxChannelsListResp | null>(null);

let refreshHandle: ReturnType<typeof setInterval> | null = null;

// --- Display helpers -----------------------------------------------------

function statusPillClass(status: PlayoutChannel["status"]): string {
  switch (status) {
    case "ACTIVE":
      return "bg-emerald-500/20 text-emerald-300 border-emerald-500/40";
    case "INACTIVE":
      return "bg-slate-700/50 text-slate-300 border-slate-600";
    case "ERROR":
      return "bg-rose-500/20 text-rose-300 border-rose-500/40";
    default:
      return "bg-slate-700/50 text-slate-300 border-slate-600";
  }
}

function isRegistered(id: string): boolean {
  return espx.value?.registered?.[id] === true;
}

function shortId(id: string): string {
  if (!id || id.length <= 8) return id ?? "";
  return `${id.slice(0, 8)}…`;
}

function showToast(kind: "success" | "error", text: string): void {
  toast.value = { kind, text };
  setTimeout(() => {
    if (toast.value?.text === text) toast.value = null;
  }, 3500);
}

// --- Data loading --------------------------------------------------------

async function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return await new Promise<T>((resolve, reject) => {
    const t = setTimeout(
      () => reject(new Error(`Request timed out after ${ms}ms`)),
      ms,
    );
    p.then(
      (v) => {
        clearTimeout(t);
        resolve(v);
      },
      (e) => {
        clearTimeout(t);
        reject(e);
      },
    );
  });
}

async function fetchLocal(): Promise<void> {
  const resp: ListChannelsResp = await withTimeout(
    listChannels({ pageSize: 100 }),
    REQUEST_TIMEOUT_MS,
  );
  localChannels.value = resp.data;
}

async function fetchEspx(): Promise<void> {
  espx.value = await withTimeout(listESpxChannels(), REQUEST_TIMEOUT_MS);
}

async function refreshAll(): Promise<void> {
  loading.value = true;
  errorMessage.value = null;
  try {
    await Promise.all([fetchLocal(), fetchEspx()]);
  } catch (err) {
    errorMessage.value =
      err instanceof Error ? err.message : "Failed to load channels";
  } finally {
    loading.value = false;
  }
}

// --- Row actions ---------------------------------------------------------

function withPending<T>(
  id: string,
  action: PendingAction,
  fn: () => Promise<T>,
): Promise<T | undefined> {
  pendingAction.value = { ...pendingAction.value, [id]: action };
  return fn()
    .catch((err) => {
      showToast(
        "error",
        err instanceof Error
          ? err.message
          : err instanceof ApiError
            ? err.message
            : "Action failed",
      );
      return undefined;
    })
    .finally(() => {
      const next = { ...pendingAction.value };
      next[id] = null;
      pendingAction.value = next;
    });
}

async function onStart(ch: PlayoutChannel): Promise<void> {
  // Optimistic flip
  const idx = localChannels.value.findIndex((c) => c.id === ch.id);
  const prevStatus = ch.status;
  const prevActive = ch.is_active;
  if (idx >= 0) {
    localChannels.value[idx] = {
      ...ch,
      status: "ACTIVE",
      is_active: true,
    };
  }
  await withPending(ch.id, "start", () => startChannel(ch.id))
    .then((updated) => {
      if (updated && idx >= 0) localChannels.value[idx] = updated;
      showToast("success", `Started ${ch.name}`);
    })
    .catch(() => {
      // Revert
      if (idx >= 0) {
        localChannels.value[idx] = {
          ...ch,
          status: prevStatus,
          is_active: prevActive,
        };
      }
    });
  // Refresh ESPX too so `registered` stays current.
  fetchEspx().catch(() => {});
}

async function onStop(ch: PlayoutChannel): Promise<void> {
  const isActive = channelStore.activeChannelId === ch.id;
  if (isActive) {
    const ok = window.confirm(
      `Stop "${ch.name}"? The player will be cleared back to the placeholder.`,
    );
    if (!ok) return;
  }

  const idx = localChannels.value.findIndex((c) => c.id === ch.id);
  const prevStatus = ch.status;
  const prevActive = ch.is_active;
  if (idx >= 0) {
    localChannels.value[idx] = {
      ...ch,
      status: "INACTIVE",
      is_active: false,
    };
  }

  if (isActive) clearActiveChannel();

  await withPending(ch.id, "stop", () => stopChannel(ch.id))
    .then((updated) => {
      if (updated && idx >= 0) localChannels.value[idx] = updated;
      showToast("success", `Stopped ${ch.name}`);
    })
    .catch(() => {
      if (idx >= 0) {
        localChannels.value[idx] = {
          ...ch,
          status: prevStatus,
          is_active: prevActive,
        };
      }
    });
}

async function onRegister(channelId: string, name: string): Promise<void> {
  await withPending(channelId, "register", () =>
    createChannel({ channel_id: channelId }),
  ).then(async (resp) => {
    if (resp) {
      showToast("success", `Registered ${name}`);
      await fetchLocal();
      await fetchEspx();
    }
  });
}

async function onSelect(ch: PlayoutChannel): Promise<void> {
  if (!ch.is_active) {
    showToast("error", `${ch.name} is not active. Start it first.`);
    return;
  }
  selectChannel(ch);
  showToast("success", `Now playing ${ch.name}`);
}

// --- URL rehydration ----------------------------------------------------
//
// On first load the URL may carry `?channel=<id>`. We resolve it once the
// local list arrives: if the channel exists, we promote the URL id into a
// full PlayoutChannel and bump the store so the player picks it up.
//
// If the channel is not yet in the list (e.g. first paint) we set just the
// id; the `watch(localChannels, ...)` below will fill in metadata as soon
// as the channel shows up after a refresh.

function rehydrateFromUrl(): void {
  const id = pendingChannelFromUrl();
  if (!id) {
    channelStore.hydratedFromUrl = true;
    return;
  }
  // If the user already selected something (e.g. HMR re-mount), keep it.
  if (channelStore.activeChannelId === id) {
    channelStore.hydratedFromUrl = true;
    return;
  }
  // Try to resolve immediately from the already-loaded list.
  const found = localChannels.value.find((c) => c.id === id);
  if (found) {
    selectChannel(found);
  } else {
    setActiveChannelId(id);
  }
  channelStore.hydratedFromUrl = true;
}

watch(
  localChannels,
  (list) => {
    if (!channelStore.hydratedFromUrl) return;
    const id = channelStore.activeChannelId;
    if (!id) return;
    if (channelStore.activeChannel?.id === id) return;
    const found = list.find((c) => c.id === id);
    if (found) {
      // Promote the placeholder id into full metadata without touching
      // the URL (it's already correct).
      channelStore.activeChannel = found;
      channelStore.changeToken += 1;
    }
  },
  { deep: true },
);

// --- Computed views ------------------------------------------------------

const sortedLocal = computed(() => {
  // Keep the active selection at the top, then sort by name.
  const arr = localChannels.value.slice();
  arr.sort((a, b) => {
    if (a.id === channelStore.activeChannelId) return -1;
    if (b.id === channelStore.activeChannelId) return 1;
    return a.name.localeCompare(b.name);
  });
  return arr;
});

const espxRows = computed(() => {
  const data = espx.value?.data ?? [];
  return data.slice().sort((a, b) => a.name.localeCompare(b.name));
});

const activeCount = computed(
  () => localChannels.value.filter((c) => c.is_active).length,
);
const totalCount = computed(() => localChannels.value.length);

// --- Lifecycle -----------------------------------------------------------

onMounted(() => {
  refreshAll().then(() => rehydrateFromUrl());
  refreshHandle = setInterval(refreshAll, REFRESH_MS);
});

onBeforeUnmount(() => {
  if (refreshHandle) clearInterval(refreshHandle);
  refreshHandle = null;
});
</script>

<template>
  <channel-list-panel
    class="block w-full h-full bg-gradient-to-b from-slate-950 via-slate-900 to-slate-950 text-slate-100"
  >
    <div class="p-4 space-y-4">
      <!-- Header -->
      <div class="flex items-baseline justify-between">
        <h2
          class="text-sm font-semibold tracking-wide uppercase text-slate-100"
        >
          Playout channels
        </h2>
        <div class="flex items-center gap-2 text-[10px] font-mono">
          <span class="px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300">
            {{ activeCount }} active
          </span>
          <span class="px-1.5 py-0.5 rounded bg-slate-800 text-slate-400">
            {{ totalCount }} total
          </span>
        </div>
      </div>

      <!-- Source toggle -->
      <div
        class="grid grid-cols-2 gap-1 p-1 rounded-lg bg-slate-900/60 border border-slate-800"
        role="tablist"
        aria-label="Channel source"
      >
        <button
          type="button"
          role="tab"
          :aria-selected="sourceTab === 'local'"
          class="px-2 py-1.5 text-xs font-semibold rounded-md transition"
          :class="
            sourceTab === 'local'
              ? 'bg-sky-500/20 text-sky-200 border border-sky-500/40'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 border border-transparent'
          "
          @click="sourceTab = 'local'"
        >
          Local
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="sourceTab === 'espx'"
          class="px-2 py-1.5 text-xs font-semibold rounded-md transition"
          :class="
            sourceTab === 'espx'
              ? 'bg-sky-500/20 text-sky-200 border border-sky-500/40'
              : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800/60 border border-transparent'
          "
          @click="sourceTab = 'espx'"
        >
          ESPX upstream
        </button>
      </div>

      <!-- Toolbar -->
      <div class="flex items-center gap-2">
        <button
          type="button"
          class="ml-auto px-2.5 py-1 text-[11px] font-semibold rounded-md bg-slate-800 hover:bg-slate-700 text-slate-200 transition"
          :disabled="loading"
          @click="refreshAll"
        >
          <span v-if="!loading">Refresh</span>
          <span v-else>Refreshing…</span>
        </button>
      </div>

      <!-- Error banner -->
      <div
        v-if="errorMessage"
        class="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-200"
      >
        {{ errorMessage }}
      </div>

      <!-- Toast -->
      <div
        v-if="toast"
        class="rounded-md border px-3 py-2 text-xs font-medium"
        :class="
          toast.kind === 'success'
            ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-200'
            : 'border-rose-500/40 bg-rose-500/10 text-rose-200'
        "
      >
        {{ toast.text }}
      </div>

      <!-- LOCAL view -->
      <div v-if="sourceTab === 'local'" class="space-y-1.5">
        <div
          v-if="loading && !localChannels.length"
          class="space-y-1.5"
          aria-busy="true"
        >
          <div
            v-for="i in 3"
            :key="i"
            class="h-14 rounded-lg bg-slate-800/40 animate-pulse"
          ></div>
        </div>

        <p
          v-else-if="!localChannels.length"
          class="text-xs text-slate-500 px-1"
        >
          No local channels yet. Switch to <strong>ESPX upstream</strong> and
          register one.
        </p>

        <ul v-else class="space-y-1.5">
          <li
            v-for="ch in sortedLocal"
            :key="ch.id"
            class="rounded-lg border p-2.5 transition"
            :class="
              channelStore.activeChannelId === ch.id
                ? 'border-sky-500/50 bg-sky-500/10'
                : 'border-slate-800 bg-slate-900/40 hover:bg-slate-900/70'
            "
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span
                    class="inline-block w-2 h-2 rounded-full flex-shrink-0"
                    :class="
                      ch.is_active
                        ? 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.7)]'
                        : 'bg-slate-600'
                    "
                    aria-hidden="true"
                  ></span>
                  <span class="text-sm font-semibold truncate" :title="ch.name">
                    {{ ch.name }}
                  </span>
                  <span
                    class="px-1.5 py-0.5 rounded-full text-[9px] font-bold tracking-wider uppercase border"
                    :class="statusPillClass(ch.status)"
                  >
                    {{ ch.status }}
                  </span>
                </div>
                <div
                  class="mt-0.5 text-[10px] font-mono text-slate-500 truncate"
                  :title="ch.id"
                >
                  {{ ch.base_name || "—" }} · {{ shortId(ch.id) }} ·
                  {{ ch.content_timezone || "UTC" }}
                </div>
              </div>
            </div>

            <div class="mt-2 flex items-center gap-1.5">
              <button
                v-if="!ch.is_active"
                type="button"
                class="px-2 py-1 text-[11px] font-semibold rounded-md bg-emerald-500/20 hover:bg-emerald-500/30 text-emerald-200 border border-emerald-500/40 transition disabled:opacity-50 disabled:cursor-wait"
                :disabled="pendingAction[ch.id] === 'start'"
                @click="onStart(ch)"
              >
                <span v-if="pendingAction[ch.id] === 'start'">Starting…</span>
                <span v-else>Start</span>
              </button>
              <button
                v-else
                type="button"
                class="px-2 py-1 text-[11px] font-semibold rounded-md bg-rose-500/15 hover:bg-rose-500/25 text-rose-200 border border-rose-500/40 transition disabled:opacity-50 disabled:cursor-wait"
                :disabled="pendingAction[ch.id] === 'stop'"
                @click="onStop(ch)"
              >
                <span v-if="pendingAction[ch.id] === 'stop'">Stopping…</span>
                <span v-else>Stop</span>
              </button>
              <button
                type="button"
                class="px-2 py-1 text-[11px] font-semibold rounded-md transition disabled:opacity-50 disabled:cursor-not-allowed"
                :class="
                  channelStore.activeChannelId === ch.id
                    ? 'bg-sky-500/30 text-sky-100 border border-sky-500/50'
                    : ch.is_active
                      ? 'bg-sky-500/15 hover:bg-sky-500/25 text-sky-200 border border-sky-500/40'
                      : 'bg-slate-800 text-slate-500 border border-slate-700'
                "
                :disabled="!ch.is_active"
                :title="
                  ch.is_active
                    ? `Watch ${ch.name}`
                    : `${ch.name} is not active`
                "
                @click="onSelect(ch)"
              >
                <span v-if="channelStore.activeChannelId === ch.id">
                  Playing
                </span>
                <span v-else>Select</span>
              </button>
            </div>
          </li>
        </ul>
      </div>

      <!-- ESPX view -->
      <div v-else class="space-y-1.5">
        <div
          v-if="loading && !espx"
          class="space-y-1.5"
          aria-busy="true"
        >
          <div
            v-for="i in 3"
            :key="i"
            class="h-12 rounded-lg bg-slate-800/40 animate-pulse"
          ></div>
        </div>

        <p v-else-if="!espx" class="text-xs text-slate-500 px-1">
          No upstream data.
        </p>

        <ul v-else class="space-y-1.5">
          <li
            v-for="row in espxRows"
            :key="row.id"
            class="rounded-lg border p-2.5 transition"
            :class="
              isRegistered(row.id)
                ? 'border-slate-800 bg-slate-900/40'
                : 'border-amber-500/30 bg-amber-500/5'
            "
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-semibold truncate" :title="row.name">
                    {{ row.name }}
                  </span>
                  <span
                    v-if="isRegistered(row.id)"
                    class="px-1.5 py-0.5 rounded-full text-[9px] font-bold tracking-wider uppercase bg-emerald-500/20 text-emerald-300 border border-emerald-500/40"
                  >
                    Registered
                  </span>
                  <span
                    v-else
                    class="px-1.5 py-0.5 rounded-full text-[9px] font-bold tracking-wider uppercase bg-amber-500/20 text-amber-200 border border-amber-500/40"
                  >
                    Not registered
                  </span>
                </div>
                <div
                  class="mt-0.5 text-[10px] font-mono text-slate-500 truncate"
                  :title="row.id"
                >
                  {{ row.base_name || "—" }} · {{ shortId(row.id) }} ·
                  {{ row.content_timezone || "UTC" }}
                </div>
              </div>
            </div>

            <div class="mt-2 flex items-center gap-1.5">
              <button
                v-if="!isRegistered(row.id)"
                type="button"
                class="px-2 py-1 text-[11px] font-semibold rounded-md bg-amber-500/20 hover:bg-amber-500/30 text-amber-100 border border-amber-500/40 transition disabled:opacity-50 disabled:cursor-wait"
                :disabled="pendingAction[row.id] === 'register'"
                @click="onRegister(row.id, row.name)"
              >
                <span v-if="pendingAction[row.id] === 'register'">
                  Registering…
                </span>
                <span v-else>Register</span>
              </button>
              <span v-else class="text-[10px] text-slate-500">
                Use the Local tab to start / play.
              </span>
            </div>
          </li>
        </ul>
      </div>
    </div>
  </channel-list-panel>
</template>

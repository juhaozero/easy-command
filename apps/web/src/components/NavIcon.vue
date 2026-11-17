<script setup lang="ts">
defineProps<{
  name: 'app' | 'library' | 'history' | 'schedule' | 'settings' | 'mock'
}>()
</script>

<template>
  <svg
    v-if="name === 'app'"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="3.5" y="4" width="17" height="16" rx="3" />
    <path d="M7.5 9h3.2L8.2 15h3.3M13.5 9H17m-3.5 3H16.2M13.5 15H17" />
  </svg>
  <svg
    v-else-if="name === 'library'"
    class="app-nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M4 5.5h5.5v13H4zM10.5 5.5H16v13h-5.5zM17.5 8.5H20v10h-2.5z" />
  </svg>
  <svg
    v-else-if="name === 'history'"
    class="app-nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3" />
    <path d="M4.5 5.5v4h4M12 8v4.5l3 1.8" />
  </svg>
  <svg
    v-else-if="name === 'schedule'"
    class="app-nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="3.5" y="5" width="17" height="15" rx="2.5" />
    <path d="M8 3.5v3M16 3.5v3M3.5 9.5h17M8 13h.5M12 13h.5M16 13h.5M8 16.5h.5M12 16.5h.5" />
  </svg>
  <svg
    v-else-if="name === 'mock'"
    class="app-nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="4" y="4" width="16" height="16" rx="3" />
    <path d="M8 9h8M8 12.5h5M8 16h3" />
  </svg>
  <svg
    v-else
    class="app-nav-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M4 7h5m5 0h6M4 17h10m5 0h1" />
    <circle cx="11.5" cy="7" r="2.5" />
    <circle cx="16.5" cy="17" r="2.5" />
  </svg>
</template>

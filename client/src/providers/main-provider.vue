<template>
  <AuthProvider>
    <slot></slot>
  </AuthProvider>
</template>
<script setup>
import AuthProvider from '@/providers/auth-provider.vue'
</script>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useUserStore } from './stores/user'

const userStore = useUserStore()

// 冷启动时如果已有 access token,主动拉一次 /me,拿到最新 role/permissions。
onMounted(async () => {
  if (userStore.accessToken && !userStore.user) {
    try {
      await userStore.fetchMe()
    } catch {
      userStore.clear()
    }
  }
})
</script>

<template>
  <el-config-provider namespace="el">
    <router-view />
  </el-config-provider>
</template>

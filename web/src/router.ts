import { createRouter, createWebHistory } from 'vue-router'
import { api } from './lib/api'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
    { path: '/', name: 'overview', component: () => import('./views/OverviewView.vue') },
    { path: '/apps', name: 'apps', component: () => import('./views/AppsView.vue') },
    { path: '/apps/new', name: 'app-new', component: () => import('./views/NewAppView.vue') },
    { path: '/apps/:id', name: 'app', component: () => import('./views/AppDetailView.vue'), props: true },
    { path: '/services', name: 'services', component: () => import('./views/ServicesView.vue') },
    { path: '/containers', name: 'containers', component: () => import('./views/ContainersView.vue') },
    { path: '/security', name: 'security', component: () => import('./views/SecurityView.vue') },
    { path: '/updates', name: 'updates', component: () => import('./views/UpdatesView.vue') },
    { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

// One auth check per load; API calls that later return 401 send the user back here too.
let checked = false
router.beforeEach(async (to) => {
  if (to.meta.public || checked) return true
  try {
    const st = await api.authState()
    if (st.scope !== 'full') return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
    checked = true
    return true
  } catch {
    return { name: 'login' }
  }
})

export function resetAuthCheck() {
  checked = false
}

import DefaultTheme from 'vitepress/theme'
import { h } from 'vue'
import { useData } from 'vitepress'
import './style.css'
import Home from './Home.vue'

export default {
  extends: DefaultTheme,
  Layout() {
    const { frontmatter } = useData()
    if (frontmatter.value.layout === 'custom-home') {
      return h(Home)
    }
    return h(DefaultTheme.Layout)
  },
}

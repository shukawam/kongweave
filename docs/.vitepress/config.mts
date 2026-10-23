import { defineConfig } from 'vitepress'

const repository = 'https://github.com/shukawam/kongweave'
const pages = {
  'README.md': '/',
  'guide.md': '/guide.html',
  'guide_ja.md': '/ja/guide.html'
}
// Match the composed Japanese characters in existing GitHub Markdown anchors.
const slugify = (text: string) => text.normalize('NFC').toLowerCase()
  .replace(/[^\p{L}\p{N}\p{M}\s_-]/gu, '').trim().replace(/\s+/g, '-')

export default defineConfig({
  title: 'Kongweave',
  description: 'Environment overlays for native decK and kongctl YAML.',
  base: '/kongweave/',
  // Exclude local notes and the Japanese source rendered by ja/guide.md.
  srcExclude: ['design.md', 'verification.md', 'guide_ja.md'],
  rewrites: {
    'README.md': 'index.md'
  },
  locales: {
    root: {
      label: 'English',
      lang: 'en',
      link: '/guide',
      themeConfig: {
        nav: [
          { text: 'Overview', link: '/' },
          { text: 'User guide', link: '/guide' },
          { text: 'Releases', link: `${repository}/releases` }
        ],
        sidebar: [
          {
            text: 'Get started',
            items: [
              { text: 'Overview', link: '/' },
              { text: 'Installation', link: '/guide#install-and-build-the-examples' },
              { text: 'Create an overlay', link: '/guide#create-an-environment-overlay' }
            ]
          },
          {
            text: 'Reference',
            items: [
              { text: 'Overlays and patches', link: '/guide#overlay-and-patch-reference' },
              { text: 'Resource selectors', link: '/guide#select-resources' },
              { text: 'Native YAML and files', link: '/guide#native-yaml-and-referenced-files' },
              { text: 'CLI', link: '/guide#cli-reference' }
            ]
          },
          {
            text: 'Work with bundles',
            items: [
              { text: 'Explain changes', link: '/guide#explain-changes' },
              { text: 'Reproducibility', link: '/guide#bundles-and-reproducibility' },
              { text: 'Validation and handoff', link: '/guide#validation-and-handoff' },
              { text: 'Troubleshooting', link: '/guide#troubleshooting' }
            ]
          }
        ]
      }
    },
    ja: {
      label: '日本語',
      lang: 'ja',
      link: '/ja/guide',
      description: 'decKとkongctlのネイティブYAMLを環境別に構成するためのガイド。',
      themeConfig: {
        nav: [
          { text: '利用ガイド', link: '/ja/guide' },
          { text: 'リリース', link: `${repository}/releases` }
        ],
        sidebar: [
          {
            text: 'はじめに',
            items: [
              { text: 'インストール', link: '/ja/guide#インストールとサンプルのビルド' },
              { text: '環境別overlayの作成', link: '/ja/guide#環境別overlayの作成' }
            ]
          },
          {
            text: 'リファレンス',
            items: [
              { text: 'Overlayとパッチ', link: '/ja/guide#overlayとパッチのリファレンス' },
              { text: 'リソースの対象指定', link: '/ja/guide#リソースの対象指定' },
              { text: 'ネイティブYAMLと参照ファイル', link: '/ja/guide#ネイティブyamlと参照ファイル' },
              { text: 'CLI', link: '/ja/guide#cliリファレンス' }
            ]
          },
          {
            text: 'バンドルの利用',
            items: [
              { text: '変更履歴の確認', link: '/ja/guide#変更履歴の確認' },
              { text: '再現性', link: '/ja/guide#バンドルと再現性' },
              { text: '検証と公式ツールへの受け渡し', link: '/ja/guide#検証と公式ツールへの受け渡し' },
              { text: 'トラブルシューティング', link: '/ja/guide#トラブルシューティング' }
            ]
          }
        ],
        outline: { label: 'このページの内容', level: [2, 3] },
        editLink: { pattern: `${repository}/edit/main/docs/guide_ja.md`, text: 'GitHubでこのページを編集' },
        sidebarMenuLabel: 'メニュー',
        returnToTopLabel: '先頭に戻る',
        darkModeSwitchLabel: 'テーマ',
        langMenuLabel: '言語を変更'
      }
    }
  },
  themeConfig: {
    i18nRouting: false,
    socialLinks: [{ icon: 'github', link: repository }],
    outline: { level: [2, 3] },
    editLink: { pattern: `${repository}/edit/main/docs/:path` },
    docFooter: { prev: false, next: false },
    search: {
      provider: 'local',
      options: {
        miniSearch: {
          options: {
            tokenize(text) {
              return Array.from(new Intl.Segmenter('ja', { granularity: 'word' }).segment(text))
                .filter((part) => part.isWordLike)
                .map((part) => part.segment)
            }
          }
        },
        locales: {
          ja: {
            translations: {
              button: { buttonText: '検索', buttonAriaLabel: 'ドキュメントを検索' },
              modal: {
                noResultsText: '検索結果がありません',
                resetButtonTitle: '検索をクリア',
                backButtonTitle: '検索を閉じる',
                displayDetails: '詳細を表示',
                footer: { selectText: '選択', navigateText: '移動', closeText: '閉じる' }
              }
            }
          }
        }
      }
    }
  },
  markdown: {
    anchor: { slugify },
    toc: { slugify },
    config(md) {
      // Keep source links usable on GitHub and in release archives. Only the
      // rendered site uses rewritten routes and GitHub links for repository files.
      md.core.ruler.after('inline', 'source-links', (state) => {
        for (const block of state.tokens) {
          for (const token of block.children ?? []) {
            if (token.type !== 'link_open') continue
            const href = token.attrGet('href') ?? ''
            const [file, ...fragment] = href.split('#')
            const suffix = fragment.length ? `#${fragment.join('#')}` : ''
            if (Object.hasOwn(pages, file)) {
              token.attrSet('href', pages[file as keyof typeof pages] + suffix)
            } else if (file.startsWith('../')) {
              const kind = file.endsWith('/') ? 'tree' : 'blob'
              token.attrSet('href', `${repository}/${kind}/main/${href.slice(3)}`)
            }
          }
        }
      })
    }
  }
})

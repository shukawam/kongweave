# Maintain the documentation site

The documentation uses [VitePress](https://vitepress.dev/) and is published to GitHub Pages at <https://shukawam.github.io/kongweave/> after Pages is enabled and the workflow succeeds.
Node.js 24 or later is required only to develop the website; the Kongweave CLI remains standalone.

## Preview locally

Run these commands from a source checkout at the repository root:

```sh
npm ci
npm run docs:dev
```

Open the `/kongweave/` URL printed by VitePress.
To check the production output:

```sh
npm run docs:build
npm run docs:preview
```

The generated site is in `docs/.vitepress/dist/` and is ignored by Git.
The build fails on unresolved internal page links.

## Edit content

- `docs/README.md` is the site's home page.
- `docs/guide.md` is the English guide at `/kongweave/guide.html`.
- `docs/guide_ja.md` is the Japanese guide, included by `docs/ja/guide.md` at `/kongweave/ja/guide.html`.
- `docs/.vitepress/config.mts` defines navigation, locales, search, and route rewrites.

Edit the original Markdown files; there are no separate website copies of the guides.
Existing relative links in these top-level Markdown files still work on GitHub and in release archives.
The site renderer maps links to the three entry pages to their public routes and maps `../` links to repository files on GitHub.
Keep site content at the top level of `docs/` when using this link convention.
Local `docs/design.md` and `docs/verification.md` notes are excluded from the site.

Keep both guides aligned when changing behavior, and update the sidebar anchors when renaming guide sections.
Local search builds its index from the published pages and requires no external search service.
The Japanese include page places its search index in the correct locale while preserving the original Markdown path.
Search uses word segmentation for both English and Japanese.

The dependency lockfile pins Vite 8.3.4 and VitePress 2.0.0-alpha.20.
VitePress 2 declares support for Vite 8, so no dependency override or separate esbuild installation is needed.
VitePress is deliberately pinned to an alpha release to use this compatible toolchain; assess future upgrades before changing the pin.
VitePress 1.x cannot be upgraded to Vite 8 through an override alone: its bundle generation and rendering rely on APIs that changed in Vite 8.
Verify dependency updates with `npm ci`, `npm run docs:build`, and `npm audit`, and check navigation, both search locales, and the local development server.
Dependabot leaves major Vite upgrades for a coordinated VitePress upgrade.

## Enable GitHub Pages

1. Open the repository's **Settings → Pages**.
2. Under **Build and deployment → Source**, select **GitHub Actions**.
3. Merge the site configuration into `main`, or run the **Documentation** workflow manually on `main` if it has already been merged.
4. Check the workflow's `deploy` job for the published URL.

The workflow builds documentation changes in pull requests without deploying them.
Changes to the documentation, dependency files, or documentation workflow on `main` trigger a build and deployment.
Only the deployment job receives Pages and OIDC write permissions.
No personal access token or `gh-pages` branch is required.

The configured base path is `/kongweave/`.
If the repository is renamed or a custom domain is used, update `base` in the VitePress configuration and the documented public URL before publishing.
For the hosting requirements, see [GitHub's custom workflow documentation](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages).

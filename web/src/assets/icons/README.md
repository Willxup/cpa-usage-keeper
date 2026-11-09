# Provider icon assets

The following SVG additions are vendored from [Lobe Icons](https://github.com/lobehub/lobe-icons/tree/8b7bbc433d38437fa2b0cca58ae896917162b812), revision `8b7bbc433d38437fa2b0cca58ae896917162b812`, under the included [MIT license](LICENSE.lobe-icons):

| Local asset | Upstream path | Avatar background / foreground / scale |
| --- | --- | --- |
| `deepseek.svg` | `packages/static-svg/icons/deepseek.svg` | `#4D6BFE` / white / `0.75` |
| `muse.svg` | `packages/static-svg/icons/muse.svg` | white / `#0082FB` / `0.75` |
| `qwen.svg` | `packages/static-svg/icons/qwen.svg` | `#082DFF` / white / `0.6` |
| `zai.svg` | `packages/static-svg/icons/zai.svg` | black / white / `0.6` |

Avatar values follow upstream `src/{DeepSeek,Muse,Qwen,ZAI}/style.ts`. `ProviderBrandIcon.module.scss` applies the shared circular container, background, scale, and white foreground inversion. The Muse SVG replaces `currentColor` with upstream `AVATAR_COLOR` (`#0082FB`) because external SVG images cannot inherit the surrounding text color. The other three SVGs are unchanged.

Muse is the Meta Muse model family, distinct from the existing generic Meta icon. GLM model entries use the Z.ai brand icon.

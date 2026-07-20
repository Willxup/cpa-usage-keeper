import { readFileSync } from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { AppShell, type AppShellSlots, type AppShellVariant } from './AppShell';

const shellStyles = readFileSync(new URL('./AppShell.module.scss', import.meta.url), 'utf8');
const themes = readFileSync(new URL('../../styles/themes.scss', import.meta.url), 'utf8');
const provider = readFileSync(new URL('../../theme/AntdProvider.tsx', import.meta.url), 'utf8');
const shellSource = readFileSync(new URL('./AppShell.tsx', import.meta.url), 'utf8');

const baseSlots: AppShellSlots = {
  brand: <span>Brand</span>,
  navigation: <nav>Nav</nav>,
  headerTitle: 'Usage',
  headerSubtitle: 'Subtitle',
  headerLeading: <button type="button">Menu</button>,
  headerActions: <button type="button">Refresh</button>,
  headerUtility: <span>Utility</span>,
  toolbar: <div>Toolbar</div>,
  content: <section>Content</section>,
  footer: <span>Footer</span>,
};

const renderVariant = (variant: AppShellVariant, slots: Partial<AppShellSlots> = {}) =>
  renderToStaticMarkup(<AppShell variant={variant} slots={{ ...baseSlots, ...slots }} />);

describe('AppShell variants', () => {
  it('marks the root with variant, density, sticky, and nav contracts', () => {
    const html = renderVariant('authenticated');

    expect(html).toContain('data-shell="app"');
    expect(html).toContain('data-variant="authenticated"');
    expect(html).toContain('data-density="standard"');
    expect(html).toContain('data-sticky="always"');
    expect(html).toContain('data-nav="sidebar"');
  });

  it('renders sidebar navigation with brand and utility only for the authenticated variant', () => {
    const authenticated = renderVariant('authenticated');
    const viewer = renderVariant('viewer');
    const guest = renderVariant('guest');
    const embed = renderVariant('embed');

    expect(authenticated).toContain('data-shell-slot="navigation"');
    expect(viewer).not.toContain('data-shell-slot="navigation"');
    expect(guest).not.toContain('data-shell-slot="navigation"');
    expect(embed).not.toContain('data-shell-slot="navigation"');
  });

  it('places the brand in the sidebar for authenticated and in the header for other variants', () => {
    const authenticated = renderVariant('authenticated');
    const viewer = renderVariant('viewer');

    const authNavIndex = authenticated.indexOf('data-shell-slot="navigation"');
    const authBrandIndex = authenticated.indexOf('Brand');
    expect(authBrandIndex).toBeGreaterThan(authNavIndex);
    expect(authBrandIndex).toBeLessThan(authenticated.indexOf('data-shell-slot="header"'));

    const viewerHeaderIndex = viewer.indexOf('data-shell-slot="header"');
    const viewerBrandIndex = viewer.indexOf('Brand');
    expect(viewerBrandIndex).toBeGreaterThan(viewerHeaderIndex);
  });

  it('declares the variant-specific sticky policy explicitly', () => {
    expect(renderVariant('authenticated')).toContain('data-sticky="always"');
    expect(renderVariant('viewer')).toContain('data-sticky="desktop"');
    expect(renderVariant('guest')).toContain('data-sticky="static"');
    expect(renderVariant('embed')).toContain('data-sticky="static"');
  });

  it('does not render a footer unless one is provided', () => {
    const withoutFooter = renderVariant('authenticated', { footer: undefined });
    const withFooter = renderVariant('authenticated');

    expect(withoutFooter).not.toContain('Footer');
    expect(withFooter).toContain('Footer');
    expect(withFooter.indexOf('data-shell-slot="content"')).toBeLessThan(withFooter.indexOf('Footer'));
  });

  it('hides header chrome for embed without navigation while keeping content', () => {
    const html = renderVariant('embed');

    expect(html).not.toContain('data-shell-slot="header"');
    expect(html).toContain('data-shell-slot="content"');
    expect(html).toContain('data-density="compact"');
  });

  it('keeps header slot order as leading, brand, title, actions, utility', () => {
    const html = renderVariant('viewer');
    const headerStart = html.indexOf('data-shell-slot="header"');
    const headerEnd = html.indexOf('data-shell-slot="content"');
    const header = html.slice(headerStart, headerEnd);

    const leadingIndex = header.indexOf('Menu');
    const brandIndex = header.indexOf('Brand');
    const titleIndex = header.indexOf('Usage');
    const actionsIndex = header.indexOf('Refresh');
    const utilityIndex = header.indexOf('Utility');

    expect(leadingIndex).toBeGreaterThanOrEqual(0);
    expect(leadingIndex).toBeLessThan(brandIndex);
    expect(brandIndex).toBeLessThan(titleIndex);
    expect(titleIndex).toBeLessThan(actionsIndex);
    expect(actionsIndex).toBeLessThan(utilityIndex);
  });

  it('renders exactly one page heading inside the header', () => {
    const html = renderVariant('viewer');

    expect(html.match(/<h1/g)).toHaveLength(1);
    expect(html).toContain('Subtitle');
  });

  it('routes the utility slot into both the sidebar and the header for the authenticated variant', () => {
    const html = renderVariant('authenticated');

    const navigationEnd = html.indexOf('data-shell-region="main"');
    expect(navigationEnd).toBeGreaterThan(0);
    expect(html.slice(0, navigationEnd)).toContain('Utility');
    expect(html.slice(navigationEnd)).toContain('Utility');
  });

  it('prefers the dedicated sidebar utility slot over the header utility in the sidebar', () => {
    const html = renderVariant('authenticated', { sidebarUtility: <span>SidebarUtility</span> });

    const navigationEnd = html.indexOf('data-shell-region="main"');
    expect(html.slice(0, navigationEnd)).toContain('SidebarUtility');
    expect(html.slice(0, navigationEnd)).not.toContain('>Utility<');
    expect(html.slice(navigationEnd)).toContain('Utility');
  });

  it('has no business or authentication side effects', () => {
    expect(shellSource).not.toContain('useEffect');
    expect(shellSource).not.toContain('useState');
    expect(shellSource).not.toContain('localStorage');
    expect(shellSource).not.toContain('fetch(');
  });
});

describe('AppShell structure styles', () => {
  it('scopes surfaces to semantic tokens instead of hard-coded colors', () => {
    expect(shellStyles).toContain('background: var(--surface-canvas);');
    expect(shellStyles).toContain('background: var(--surface-content);');
    expect(shellStyles).toContain('background: var(--surface-nav);');
    expect(shellStyles).toContain('background: var(--surface-header);');
    expect(shellStyles).toContain('border-bottom: 1px solid var(--border-structural);');
    expect(shellStyles).toContain('border-inline-end: 1px solid var(--border-structural);');
    expect(shellStyles).not.toMatch(/#[0-9a-fA-F]{3,8}\b/);
  });

  it('keeps max-width, gutter, sticky, and breakpoint ownership in the shell stylesheet', () => {
    expect(shellStyles).toContain('width: min(var(--page-max-width), 100%);');
    expect(shellStyles).toContain('padding: var(--page-gutter);');
    expect(shellStyles).toMatch(/\.shellHeaderStickyAlways,[\s\S]*?\.shellHeaderStickyDesktop\s*\{[\s\S]*?position:\s*sticky;/);
    expect(shellStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.shellHeaderStickyDesktop\s*\{[\s\S]*?position:\s*static;/);
    expect(shellStyles).toMatch(/@include mobile\s*\{[\s\S]*?\.shellNavigation\s*\{[\s\S]*?display:\s*none;/);
    expect(shellStyles).toContain('gap: var(--page-stack);');
  });

  it('does not restyle Ant Design internals from the shell stylesheet', () => {
    expect(shellStyles).not.toContain('.ant-');
    expect(shellStyles).not.toContain(':global');
  });
});

describe('semantic surface token foundation', () => {
  it('defines the layered surface scale plus structural and control borders, elevation, and radius', () => {
    ;[
      '--surface-canvas:',
      '--surface-frame:',
      '--surface-nav:',
      '--surface-header:',
      '--surface-content:',
      '--surface-raised:',
      '--surface-overlay:',
      '--surface-inset:',
      '--border-structural:',
      '--border-subtle:',
      '--border-control:',
      '--border-focus:',
      '--elevation-card:',
      '--elevation-raised:',
      '--elevation-overlay:',
      '--radius-sm: 6px;',
      '--radius-md: 10px;',
      '--radius-lg: 14px;',
      '--radius-pill: 999px;',
    ].forEach((token) => expect(themes).toContain(token));
  });

  it('maps legacy background aliases onto semantic surfaces', () => {
    expect(themes).toMatch(/:root\s*\{[\s\S]*?--bg-secondary:\s*var\(--surface-content\);/);
    expect(themes).toMatch(/:root\s*\{[\s\S]*?--bg-primary:\s*var\(--surface-raised\);/);
    expect(themes).toMatch(/:root\s*\{[\s\S]*?--bg-quinary:\s*var\(--surface-canvas\);/);
  });

  it('gives the white theme a gray canvas instead of an all-white layout', () => {
    expect(themes).toMatch(/\[data-theme='white'\]\s*\{[\s\S]*?--surface-canvas:\s*#e9eef5;/);
    expect(themes).toMatch(/\[data-theme='white'\]\s*\{[\s\S]*?--surface-content:\s*#f4f7fb;/);
    expect(themes).toMatch(/\[data-theme='white'\]\s*\{[\s\S]*?--surface-raised:\s*#ffffff;/);
    expect(themes).not.toMatch(/\[data-theme='white'\]\s*\{[\s\S]*?--bg-secondary:\s*#ffffff;/);
  });

  it('keeps dark elevation on a black alpha ladder', () => {
    expect(themes).toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?--elevation-card:\s*0 1px 2px rgba\(0, 0, 0/);
    expect(themes).toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?--elevation-overlay:\s*0 12px 36px rgba\(0, 0, 0/);
    expect(themes).not.toMatch(/\[data-theme='dark'\]\s*\{[\s\S]*?rgba\(31, 35, 41/);
  });

  it('maps semantic surfaces into Ant Design layout, container, and border tokens', () => {
    expect(provider).toContain("colorBgLayout: 'var(--surface-content)'");
    expect(provider).toContain("colorBgContainer: 'var(--surface-raised)'");
    expect(provider).toContain("colorBgElevated: 'var(--surface-overlay)'");
    expect(provider).toContain("colorBorder: 'var(--border-structural)'");
    expect(provider).toContain("colorBorderSecondary: 'var(--border-subtle)'");
    expect(provider).toContain("boxShadow: 'var(--elevation-raised)'");
    expect(provider).toContain("boxShadowSecondary: 'var(--elevation-overlay)'");
    expect(provider).toContain("bodyBg: 'var(--surface-content)'");
    expect(provider).toContain("headerBg: 'var(--surface-header)'");
    expect(provider).toContain("siderBg: 'var(--surface-nav)'");
    expect(provider).not.toContain('isWhite');
  });
});

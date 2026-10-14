import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Follows the order a reader adopts the package in: the shared interfaces
 * first, then picking an algorithm, then per-key and composite limiting, then
 * putting it in front of HTTP, then scaling out and operating it.
 */
const sidebars: SidebarsConfig = {
  docsSidebar: [
    'intro',
    'getting-started',
    'concepts',
    {
      type: 'category',
      label: 'Algorithms',
      collapsed: false,
      items: [
        'algorithms',
        'token-bucket',
        'leaky-bucket',
        'fixed-window',
        'sliding-window',
      ],
    },
    {
      type: 'category',
      label: 'Composition',
      collapsed: false,
      items: ['keyed-limiters', 'multi'],
    },
    {
      type: 'category',
      label: 'HTTP',
      collapsed: false,
      items: ['middleware', 'key-functions', 'path-limits'],
    },
    {
      type: 'category',
      label: 'Distributed',
      collapsed: false,
      items: ['redis'],
    },
    {
      type: 'category',
      label: 'Operations',
      collapsed: false,
      items: ['metrics', 'errors'],
    },
  ],
};

export default sidebars;

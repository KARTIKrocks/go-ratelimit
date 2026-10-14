import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import type { ReactNode } from 'react';

import styles from './index.module.css';

type Feature = {
  readonly title: string;
  readonly description: string;
};

type Algorithm = {
  readonly name: string;
  readonly to: string;
  readonly bursts: string;
  readonly memory: string;
  readonly redis: boolean;
};

type Stat = {
  readonly label: string;
  readonly value: string;
  readonly unit: string;
  readonly context: string;
};

const FEATURES = [
  {
    title: 'Five Algorithms',
    description:
      'Token bucket, leaky bucket, fixed window, sliding window log, and sliding window counter',
  },
  {
    title: 'Per-Key Limiting',
    description:
      'Independent limits per user, IP, or API key, with idle cleanup and an LRU cap on tracked keys',
  },
  {
    title: 'Distributed with Redis',
    description:
      'Atomic Lua scripts, Redis server time, and Cluster-safe keys for limits shared across instances',
  },
  {
    title: 'HTTP Middleware',
    description:
      'Drop-in net/http middleware with standard X-RateLimit-* and Retry-After headers',
  },
  {
    title: 'Proxy-Aware Keys',
    description:
      'RemoteAddr by default; opt-in X-Forwarded-For handling that only trusts the proxies you list',
  },
  {
    title: 'Fail Open or Closed',
    description:
      'Choose what happens when Redis is unreachable, and observe every error through a handler',
  },
  {
    title: 'Composite Limits',
    description:
      'Combine per-second, per-minute, and per-hour limits that are checked before any is consumed',
  },
  {
    title: 'Prometheus Metrics',
    description:
      'Instrumented wrappers count allowed, denied, and errored checks per named limiter',
  },
  {
    title: 'Zero Dependencies',
    description:
      'The core package is stdlib only; Redis and Prometheus live in their own modules',
  },
] as const satisfies readonly Feature[];

// Kept in sync with the comparison table on the Algorithms docs page.
const ALGORITHMS = [
  {
    name: 'Token bucket',
    to: '/docs/token-bucket',
    bursts: 'Up to burst size',
    memory: 'Constant',
    redis: true,
  },
  {
    name: 'Leaky bucket',
    to: '/docs/leaky-bucket',
    bursts: 'Up to capacity',
    memory: 'Constant',
    redis: false,
  },
  {
    name: 'Fixed window',
    to: '/docs/fixed-window',
    bursts: 'Up to 2× limit at a boundary',
    memory: 'Constant',
    redis: true,
  },
  {
    name: 'Sliding window log',
    to: '/docs/sliding-window',
    bursts: 'None — exact',
    memory: 'One timestamp per request',
    redis: false,
  },
  {
    name: 'Sliding window counter',
    to: '/docs/sliding-window',
    bursts: 'Approximate smoothing',
    memory: 'Constant',
    redis: true,
  },
] as const satisfies readonly Algorithm[];

// Measured on an Intel i5-11400H @ 2.70GHz (12 cores), Go 1.27, Linux, with
// `go test -bench=. -benchmem` in the repository root.
const STATS = [
  {
    label: 'TokenBucket.Allow',
    value: '50',
    unit: 'ns/op',
    context: '0 allocs, single goroutine',
  },
  {
    label: 'KeyedTokenBucket.Allow',
    value: '74',
    unit: 'ns/op',
    context: '0 allocs, including the key lookup',
  },
  {
    label: 'Contended Allow',
    value: '104',
    unit: 'ns/op',
    context: '0 allocs, 12 goroutines on one bucket',
  },
  {
    label: 'SlidingWindowCounter',
    value: '74',
    unit: 'ns/op',
    context: '0 allocs, single goroutine',
  },
] as const satisfies readonly Stat[];

const INSTALL_COMMAND = 'go get github.com/KARTIKrocks/go-ratelimit';

const REPO_URL = 'https://github.com/KARTIKrocks/go-ratelimit';

function Hero(): ReactNode {
  return (
    <header className={styles.hero}>
      <div className="container">
        <h1 className={styles.title}>Production-ready rate limiting for Go</h1>
        <p className={styles.subtitle}>
          Five rate limiting algorithms, per-key limits, Redis-backed
          distributed limiters, and net/http middleware — with a zero-dependency
          core and concurrency-safe, allocation-free hot paths.
        </p>

        <div className={styles.buttons}>
          <Link
            className="button button--primary button--lg"
            to="/docs/getting-started">
            Get Started
          </Link>
          <Link
            className="button button--secondary button--lg"
            to="https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit">
            API Reference
          </Link>
        </div>

        <div className={styles.install}>
          <span className={styles.prompt} aria-hidden="true">
            $
          </span>
          <code>{INSTALL_COMMAND}</code>
        </div>
      </div>
    </header>
  );
}

function Features(): ReactNode {
  return (
    <section className="container" aria-label="Features">
      <div className={styles.features}>
        {FEATURES.map((feature) => (
          <article key={feature.title} className={styles.card}>
            <h2>{feature.title}</h2>
            <p>{feature.description}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

function Algorithms(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <h2 className={styles.sectionTitle}>Pick an algorithm</h2>
        <p className={styles.sectionLead}>
          Every algorithm implements the same interfaces, so switching is a
          one-line change. The trade-off is how much burst you tolerate against
          how much state you keep per key.
        </p>

        <div className={styles.tableScroll}>
          <table className={styles.compare}>
            <thead>
              <tr>
                <th scope="col">Algorithm</th>
                <th scope="col">Bursts</th>
                <th scope="col">Memory per key</th>
                <th scope="col">Redis</th>
              </tr>
            </thead>
            <tbody>
              {ALGORITHMS.map((algorithm) => (
                <tr key={algorithm.name}>
                  <th scope="row">
                    <Link to={algorithm.to}>{algorithm.name}</Link>
                  </th>
                  <td>{algorithm.bursts}</td>
                  <td>{algorithm.memory}</td>
                  <td>
                    {algorithm.redis ? (
                      <>
                        <span className={styles.check} aria-hidden="true">
                          ✓
                        </span>
                        <span className={styles.srOnly}>Available</span>
                      </>
                    ) : (
                      <span className={styles.diy}>In-memory only</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

function Performance(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <h2 className={styles.sectionTitle}>Performance</h2>
        <p className={styles.sectionLead}>
          Measured on an Intel i5-11400H (12 cores), Go 1.27, Linux. Every
          in-memory limiter decides a request without allocating.
        </p>

        <div className={styles.stats}>
          {STATS.map((stat) => (
            <article key={stat.label} className={styles.stat}>
              <h3 className={styles.statLabel}>{stat.label}</h3>
              <p className={styles.statValue}>
                {stat.value}
                <span className={styles.statUnit}> {stat.unit}</span>
              </p>
              <p className={styles.statContext}>{stat.context}</p>
            </article>
          ))}
        </div>

        <p className={styles.sectionNote}>
          Reproduce them yourself with{' '}
          <code>go test -bench=. -benchmem ./...</code> in the repository root.
          The <Link to={`${REPO_URL}#benchmarks`}>full benchmark table</Link>{' '}
          covers every algorithm and its keyed variant.
        </p>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const { siteConfig } = useDocusaurusContext();

  return (
    <Layout
      title={siteConfig.tagline}
      description="A production-ready rate limiting library for Go with token bucket, leaky bucket, fixed and sliding window algorithms, Redis-backed distributed limits, and HTTP middleware.">
      <Hero />
      <main>
        <Features />
        <Algorithms />
        <Performance />
      </main>
    </Layout>
  );
}

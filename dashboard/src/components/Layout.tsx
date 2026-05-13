import { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { BoltIcon } from '@heroicons/react/24/outline'
import ConnectionStatus from './ConnectionStatus'
import ThemeToggle from './ThemeToggle'

interface LayoutProps {
  children: ReactNode
}

const navItems = [
  { name: 'Overview', href: '/' },
]

export default function Layout({ children }: LayoutProps) {
  return (
    <div className="min-h-screen bg-gray-50 dark:bg-slate-950">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 bg-primary-600 text-white px-4 py-2 rounded-md z-50 transition-all"
      >
        Skip to main content
      </a>

      <header
        className="sticky top-0 z-40 border-b border-gray-200 bg-white/90 backdrop-blur dark:border-slate-800 dark:bg-slate-950/90"
        role="banner"
      >
        <div className="mx-auto flex h-14 max-w-[1500px] items-center justify-between gap-4 px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-6 min-w-0">
            <Link to="/" className="flex items-center gap-2 group min-w-0">
              <span className="flex h-8 w-8 items-center justify-center rounded-md bg-primary-600 text-white shadow-sm transition-transform group-hover:scale-105">
                <BoltIcon className="h-4 w-4" />
              </span>
              <span className="flex flex-col leading-tight min-w-0">
                <span className="text-sm font-semibold text-gray-900 dark:text-slate-100 truncate">
                  JSON-RPC Bench
                </span>
                <span className="text-[10px] uppercase tracking-wider text-gray-500 dark:text-slate-500">
                  Client performance & regression tracker
                </span>
              </span>
            </Link>

            <nav aria-label="Main" className="hidden sm:flex items-center gap-1">
              {navItems.map(item => (
                <NavLink
                  key={item.name}
                  to={item.href}
                  end
                  className={({ isActive }) =>
                    `inline-flex h-8 items-center rounded px-3 text-sm font-medium transition-colors ${
                      isActive
                        ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300'
                        : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100'
                    }`
                  }
                >
                  {item.name}
                </NavLink>
              ))}
            </nav>
          </div>

          <div className="flex items-center gap-3">
            <ConnectionStatus />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main
        id="main-content"
        className="mx-auto max-w-[1500px] px-4 py-6 sm:px-6 lg:px-8"
        role="main"
        tabIndex={-1}
      >
        {children}
      </main>
    </div>
  )
}

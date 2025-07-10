import { Routes, Route } from 'react-router-dom'
import { Suspense, useEffect } from 'react'
import { Helmet } from 'react-helmet-async'
import Layout from './components/Layout'
import LoadingSpinner from './components/LoadingSpinner'
import ErrorBoundary from './components/ErrorBoundary'

// Lazy load pages for better performance
import { lazy } from 'react'

const Dashboard = lazy(() => import('./pages/Dashboard'))
const RunDetails = lazy(() => import('./pages/RunDetails'))
const NotFound = lazy(() => import('./pages/NotFound'))

function App() {
  // Set up keyboard navigation for accessibility
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Skip to main content with Alt+M
      if (event.altKey && event.key === 'm') {
        const main = document.querySelector('main')
        if (main) {
          main.focus()
          event.preventDefault()
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [])

  return (
    <>
      <Helmet
        defaultTitle="JSON-RPC Benchmark Dashboard"
        titleTemplate="%s | JSON-RPC Benchmark Dashboard"
      >
        <meta name="description" content="Track and analyze JSON-RPC benchmark performance over time" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
      </Helmet>
      
      <Layout>
        <ErrorBoundary>
          <Suspense fallback={<LoadingSpinner />}>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/runs/:id" element={<RunDetails />} />
              <Route path="*" element={<NotFound />} />
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </Layout>
    </>
  )
}

export default App
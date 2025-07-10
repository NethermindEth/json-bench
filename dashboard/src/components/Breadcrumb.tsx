import { Link, useLocation } from 'react-router-dom'
import { ChevronRightIcon, HomeIcon } from '@heroicons/react/24/outline'

interface BreadcrumbItem {
  label: string
  href?: string
}

interface BreadcrumbProps {
  items?: BreadcrumbItem[]
}

// Helper function to generate breadcrumbs from pathname
function generateBreadcrumbs(pathname: string): BreadcrumbItem[] {
  const paths = pathname.split('/').filter(Boolean)
  const breadcrumbs: BreadcrumbItem[] = [{ label: 'Dashboard', href: '/' }]

  let currentPath = ''
  paths.forEach((path, index) => {
    currentPath += `/${path}`
    
    // Create appropriate labels for known paths
    if (path === 'compare') {
      breadcrumbs.push({ label: 'Compare Runs', href: currentPath })
    } else if (path === 'baselines') {
      breadcrumbs.push({ label: 'Baselines', href: currentPath })
    } else if (path === 'runs') {
      breadcrumbs.push({ label: 'Run Details', href: undefined }) // Don't link to runs index
    } else if (paths[index - 1] === 'runs') {
      // This is a run ID
      breadcrumbs.push({ label: `Run ${path.substring(0, 8)}...`, href: undefined })
    } else {
      // Fallback for unknown paths
      const label = path.charAt(0).toUpperCase() + path.slice(1)
      breadcrumbs.push({ label, href: index === paths.length - 1 ? undefined : currentPath })
    }
  })

  return breadcrumbs
}

export default function Breadcrumb({ items }: BreadcrumbProps) {
  const location = useLocation()
  const breadcrumbs = items || generateBreadcrumbs(location.pathname)

  if (breadcrumbs.length <= 1) {
    return null
  }

  return (
    <nav 
      className="flex mb-6"
      aria-label="Breadcrumb"
    >
      <ol className="inline-flex items-center space-x-1 md:space-x-3">
        {breadcrumbs.map((item, index) => (
          <li key={index} className="inline-flex items-center">
            {index > 0 && (
              <ChevronRightIcon className="h-4 w-4 text-gray-400 mx-1" />
            )}
            
            {item.href ? (
              <Link
                to={item.href}
                className="inline-flex items-center text-sm font-medium text-gray-500 hover:text-primary-600 transition-colors"
              >
                {index === 0 && <HomeIcon className="h-4 w-4 mr-1" />}
                {item.label}
              </Link>
            ) : (
              <span className="inline-flex items-center text-sm font-medium text-gray-900">
                {index === 0 && <HomeIcon className="h-4 w-4 mr-1" />}
                {item.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}

export type { BreadcrumbItem, BreadcrumbProps }
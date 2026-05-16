import React, { useState, useRef, useEffect } from 'react'
import { ChevronDownIcon, ChevronUpIcon } from '@heroicons/react/24/outline'
import LoadingSpinner from '../LoadingSpinner'

export interface ExpandableSectionProps {
  title: string
  subtitle?: string
  defaultExpanded?: boolean
  children: React.ReactNode
  headerActions?: React.ReactNode
  isLoading?: boolean
  error?: string
  className?: string
  disabled?: boolean
  variant?: 'default' | 'success' | 'warning' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  collapsible?: boolean
  onToggle?: (expanded: boolean) => void
}

const getVariantStyles = (variant: ExpandableSectionProps['variant'] = 'default') => {
  switch (variant) {
    case 'success':
      return {
        header: 'bg-success-50 border-success-200 dark:bg-success-900/30 dark:border-success-800',
        title: 'text-success-700 dark:text-success-300',
        subtitle: 'text-success-600 dark:text-success-400',
        icon: 'text-success-600 dark:text-success-300',
      }
    case 'warning':
      return {
        header: 'bg-warning-50 border-warning-200 dark:bg-warning-900/30 dark:border-warning-800',
        title: 'text-warning-700 dark:text-warning-300',
        subtitle: 'text-warning-600 dark:text-warning-400',
        icon: 'text-warning-600 dark:text-warning-300',
      }
    case 'danger':
      return {
        header: 'bg-danger-50 border-danger-200 dark:bg-danger-900/30 dark:border-danger-800',
        title: 'text-danger-700 dark:text-danger-300',
        subtitle: 'text-danger-600 dark:text-danger-400',
        icon: 'text-danger-600 dark:text-danger-300',
      }
    default:
      return {
        header: 'bg-gray-50 border-gray-200 dark:bg-slate-900/60 dark:border-slate-700',
        title: 'text-gray-900 dark:text-slate-100',
        subtitle: 'text-gray-600 dark:text-slate-400',
        icon: 'text-gray-500 dark:text-slate-400',
      }
  }
}

const getSizeStyles = (size: ExpandableSectionProps['size'] = 'md') => {
  switch (size) {
    case 'sm':
      return {
        header: 'px-4 py-3',
        content: 'px-4 py-3',
        title: 'text-sm font-medium',
        subtitle: 'text-xs',
        icon: 'h-4 w-4',
      }
    case 'lg':
      return {
        header: 'px-6 py-5',
        content: 'px-6 py-5',
        title: 'text-lg font-semibold',
        subtitle: 'text-base',
        icon: 'h-6 w-6',
      }
    default:
      return {
        header: 'px-5 py-4',
        content: 'px-5 py-4',
        title: 'text-base font-medium',
        subtitle: 'text-sm',
        icon: 'h-5 w-5',
      }
  }
}

export function ExpandableSection({
  title,
  subtitle,
  defaultExpanded = false,
  children,
  headerActions,
  isLoading = false,
  error,
  className = '',
  disabled = false,
  variant = 'default',
  size = 'md',
  collapsible = true,
  onToggle,
}: ExpandableSectionProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)

  const variantStyles = getVariantStyles(variant)
  const sizeStyles = getSizeStyles(size)

  const handleToggle = () => {
    if (disabled || !collapsible) return
    
    const newExpanded = !isExpanded
    setIsExpanded(newExpanded)
    onToggle?.(newExpanded)
  }

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      handleToggle()
    }
  }

  const Icon = isExpanded ? ChevronUpIcon : ChevronDownIcon

  return (
    <div className={`card ${className}`}>
      {/* Header */}
      <div
        className={`
          ${variantStyles.header} 
          ${sizeStyles.header} 
          border-b
          ${collapsible && !disabled ? 'cursor-pointer hover:bg-opacity-80' : ''}
          ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
          transition-colors duration-200
        `}
        onClick={handleToggle}
        onKeyDown={handleKeyDown}
        role={collapsible ? 'button' : undefined}
        tabIndex={collapsible && !disabled ? 0 : undefined}
        aria-expanded={collapsible ? isExpanded : undefined}
        aria-controls={collapsible ? 'expandable-content' : undefined}
        aria-label={collapsible ? `${isExpanded ? 'Collapse' : 'Expand'} ${title}` : undefined}
      >
        <div className="flex items-center justify-between">
          <div className="flex-1 min-w-0">
            <div className={`${sizeStyles.title} ${variantStyles.title}`}>
              {title}
            </div>
            {subtitle && (
              <div className={`${sizeStyles.subtitle} ${variantStyles.subtitle} mt-1`}>
                {subtitle}
              </div>
            )}
          </div>

          <div className="flex items-center space-x-3">
            {/* Loading state */}
            {isLoading && (
              <LoadingSpinner size="sm" />
            )}

            {/* Error state */}
            {error && (
              <div className="text-danger-600 text-sm font-medium">
                {error}
              </div>
            )}

            {/* Header actions — interactive controls live here, so we have to
                stop click/keydown from bubbling up to the toggle. Without
                this, opening a <select> or clicking an Export button also
                collapses the section. */}
            {headerActions && (
              <div
                className="flex items-center space-x-2"
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => e.stopPropagation()}
              >
                {headerActions}
              </div>
            )}

            {/* Toggle icon */}
            {collapsible && (
              <Icon
                className={`
                  ${sizeStyles.icon} 
                  ${variantStyles.icon} 
                  transition-transform duration-200
                  ${disabled ? 'opacity-50' : ''}
                `}
                aria-hidden="true"
              />
            )}
          </div>
        </div>
      </div>

      {/* Content */}
      {isExpanded ? (
        <div
          id="expandable-content"
          className="animate-fade-in"
          aria-hidden={false}
        >
          <div className={sizeStyles.content}>
            {children}
          </div>
        </div>
      ) : (
        <div
          id="expandable-content"
          className="overflow-hidden transition-all duration-300 ease-in-out"
          style={{
            height: 0,
            opacity: 0,
          }}
          aria-hidden={true}
        />
      )}
    </div>
  )
}

export default ExpandableSection
import React from 'react'
import { 
  ArrowUpIcon, 
  ArrowDownIcon, 
  MinusIcon,
  ChartBarIcon,
  ClockIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon
} from '@heroicons/react/24/outline'
import LoadingSpinner from './LoadingSpinner'

export interface MetricCardProps {
  title: string
  value: number | string
  unit?: string
  previousValue?: number
  percentageChange?: number
  trend?: 'up' | 'down' | 'stable'
  trendLabel?: string
  icon?: React.ComponentType<React.SVGProps<SVGSVGElement>>
  loading?: boolean
  error?: string
  className?: string
  size?: 'sm' | 'md' | 'lg'
  variant?: 'default' | 'success' | 'warning' | 'danger'
  subtitle?: string
  onClick?: () => void
  format?: 'number' | 'percentage' | 'duration' | 'bytes' | 'currency'
  precision?: number
}

const formatValue = (
  value: number | string, 
  format: MetricCardProps['format'] = 'number',
  precision: number = 2
): string => {
  if (typeof value === 'string') return value
  
  switch (format) {
    case 'percentage':
      return `${(value * 100).toFixed(precision)}%`
    case 'duration':
      if (value < 1000) return `${value.toFixed(precision)}ms`
      if (value < 60000) return `${(value / 1000).toFixed(precision)}s`
      return `${(value / 60000).toFixed(precision)}m`
    case 'bytes':
      const units = ['B', 'KB', 'MB', 'GB', 'TB']
      let size = value
      let unitIndex = 0
      while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024
        unitIndex++
      }
      return `${size.toFixed(precision)} ${units[unitIndex]}`
    case 'currency':
      return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: precision,
        maximumFractionDigits: precision,
      }).format(value)
    case 'number':
    default:
      return new Intl.NumberFormat('en-US', {
        minimumFractionDigits: precision,
        maximumFractionDigits: precision,
      }).format(value)
  }
}

const getTrendIcon = (trend?: 'up' | 'down' | 'stable') => {
  switch (trend) {
    case 'up':
      return ArrowUpIcon
    case 'down':
      return ArrowDownIcon
    case 'stable':
    default:
      return MinusIcon
  }
}

const getTrendColor = (trend?: 'up' | 'down' | 'stable', variant?: MetricCardProps['variant']) => {
  // For success/danger variants, we might want to invert the trend colors
  const isInverted = variant === 'danger'
  
  switch (trend) {
    case 'up':
      return isInverted ? 'text-danger-600' : 'text-success-600'
    case 'down':
      return isInverted ? 'text-success-600' : 'text-danger-600'
    case 'stable':
    default:
      return 'text-gray-500'
  }
}

const getVariantStyles = (variant?: MetricCardProps['variant']) => {
  switch (variant) {
    case 'success':
      return {
        card: 'border-success-200 bg-success-50',
        title: 'text-success-700',
        value: 'text-success-900',
        icon: 'text-success-600',
      }
    case 'warning':
      return {
        card: 'border-warning-200 bg-warning-50',
        title: 'text-warning-700',
        value: 'text-warning-900',
        icon: 'text-warning-600',
      }
    case 'danger':
      return {
        card: 'border-danger-200 bg-danger-50',
        title: 'text-danger-700',
        value: 'text-danger-900',
        icon: 'text-danger-600',
      }
    case 'default':
    default:
      return {
        card: 'border-gray-200 bg-white',
        title: 'text-gray-600',
        value: 'text-gray-900',
        icon: 'text-gray-500',
      }
  }
}

const getSizeStyles = (size?: MetricCardProps['size']) => {
  switch (size) {
    case 'sm':
      return {
        card: 'p-4',
        title: 'text-sm',
        value: 'text-xl font-semibold',
        icon: 'h-5 w-5',
        trend: 'text-xs',
      }
    case 'lg':
      return {
        card: 'p-8',
        title: 'text-lg',
        value: 'text-4xl font-bold',
        icon: 'h-8 w-8',
        trend: 'text-base',
      }
    case 'md':
    default:
      return {
        card: 'p-6',
        title: 'text-base',
        value: 'text-2xl font-semibold',
        icon: 'h-6 w-6',
        trend: 'text-sm',
      }
  }
}

export function MetricCard({
  title,
  value,
  unit,
  previousValue,
  percentageChange,
  trend,
  trendLabel,
  icon: Icon = ChartBarIcon,
  loading = false,
  error,
  className = '',
  size = 'md',
  variant = 'default',
  subtitle,
  onClick,
  format = 'number',
  precision = 2,
}: MetricCardProps) {
  const variantStyles = getVariantStyles(variant)
  const sizeStyles = getSizeStyles(size)
  const TrendIcon = getTrendIcon(trend)
  const trendColor = getTrendColor(trend, variant)

  const formattedValue = formatValue(value, format, precision)
  const formattedPreviousValue = previousValue !== undefined 
    ? formatValue(previousValue, format, precision) 
    : undefined

  const isClickable = !!onClick

  if (loading) {
    return (
      <div className={`card ${variantStyles.card} ${sizeStyles.card} ${className}`}>
        <div className="flex items-center justify-center h-24">
          <LoadingSpinner size="md" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className={`card ${variantStyles.card} ${sizeStyles.card} ${className}`}>
        <div className="flex items-center space-x-3">
          <ExclamationTriangleIcon className={`${sizeStyles.icon} text-danger-600`} />
          <div className="flex-1 min-w-0">
            <div className={`${sizeStyles.title} ${variantStyles.title} font-medium`}>
              {title}
            </div>
            <div className="text-danger-600 text-sm mt-1">
              {error}
            </div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div
      className={`card ${variantStyles.card} ${sizeStyles.card} ${className} ${
        isClickable ? 'cursor-pointer hover:shadow-md transition-shadow' : ''
      }`}
      onClick={onClick}
      role={isClickable ? 'button' : undefined}
      tabIndex={isClickable ? 0 : undefined}
      onKeyDown={isClickable ? (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onClick?.()
        }
      } : undefined}
    >
      <div className="flex items-start justify-between">
        <div className="flex-1 min-w-0">
          {/* Title */}
          <div className={`${sizeStyles.title} ${variantStyles.title} font-medium truncate`}>
            {title}
          </div>

          {/* Subtitle */}
          {subtitle && (
            <div className={`text-gray-500 ${size === 'sm' ? 'text-xs' : 'text-sm'} mt-1`}>
              {subtitle}
            </div>
          )}

          {/* Value */}
          <div className={`${sizeStyles.value} ${variantStyles.value} mt-2 flex items-baseline`}>
            <span>{formattedValue}</span>
            {unit && (
              <span className={`ml-1 ${size === 'sm' ? 'text-sm' : 'text-lg'} font-normal text-gray-500`}>
                {unit}
              </span>
            )}
          </div>

          {/* Trend information */}
          {(trend || percentageChange !== undefined || trendLabel) && (
            <div className="mt-3 flex items-center space-x-2">
              {trend && (
                <div className={`flex items-center ${trendColor}`}>
                  <TrendIcon className={`${size === 'sm' ? 'h-3 w-3' : 'h-4 w-4'} mr-1`} />
                </div>
              )}

              {percentageChange !== undefined && (
                <span className={`${sizeStyles.trend} font-medium ${trendColor}`}>
                  {percentageChange > 0 ? '+' : ''}{percentageChange.toFixed(1)}%
                </span>
              )}

              {trendLabel && (
                <span className={`${sizeStyles.trend} text-gray-500`}>
                  {trendLabel}
                </span>
              )}

              {formattedPreviousValue && (
                <span className={`${sizeStyles.trend} text-gray-400`}>
                  from {formattedPreviousValue}
                </span>
              )}
            </div>
          )}
        </div>

        {/* Icon */}
        <div className="flex-shrink-0 ml-4">
          <Icon className={`${sizeStyles.icon} ${variantStyles.icon}`} />
        </div>
      </div>
    </div>
  )
}

// Preset components for common metrics
export function LatencyCard(props: Omit<MetricCardProps, 'format' | 'icon'>) {
  return (
    <MetricCard
      {...props}
      format="duration"
      icon={ClockIcon}
    />
  )
}

export function SuccessRateCard(props: Omit<MetricCardProps, 'format' | 'icon' | 'variant'>) {
  const successRate = typeof props.value === 'number' ? props.value : 0
  const variant = successRate >= 0.99 ? 'success' : successRate >= 0.95 ? 'warning' : 'danger'
  
  return (
    <MetricCard
      {...props}
      format="percentage"
      icon={CheckCircleIcon}
      variant={variant}
    />
  )
}

export function ThroughputCard(props: Omit<MetricCardProps, 'format' | 'icon'>) {
  return (
    <MetricCard
      {...props}
      format="number"
      icon={ChartBarIcon}
      unit="req/s"
    />
  )
}

export default MetricCard
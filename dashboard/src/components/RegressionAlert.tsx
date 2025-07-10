import { useState } from 'react'
import {
  ExclamationTriangleIcon,
  XMarkIcon,
  ChevronRightIcon,
  CheckIcon,
  ClockIcon,
  ArrowTopRightOnSquareIcon,
} from '@heroicons/react/24/outline'
import { Regression } from '../types/api'

export interface RegressionAlertProps {
  regression: Regression
  onDismiss?: (regressionId: string) => void
  onAcknowledge?: (regressionId: string) => void
  onViewDetails?: (regression: Regression) => void
  dismissed?: boolean
  acknowledged?: boolean
  className?: string
}

/**
 * RegressionAlert component displays alerts for performance regressions
 * with severity-based styling and dismissal/acknowledgment functionality
 */
export default function RegressionAlert({
  regression,
  onDismiss,
  onAcknowledge,
  onViewDetails,
  dismissed = false,
  acknowledged = false,
  className = '',
}: RegressionAlertProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const [isAnimatingOut, setIsAnimatingOut] = useState(false)

  const getSeverityConfig = (severity: Regression['severity']) => {
    switch (severity) {
      case 'critical':
        return {
          bgColor: 'bg-danger-50',
          borderColor: 'border-danger-200',
          iconColor: 'text-danger-500',
          textColor: 'text-danger-800',
          badgeColor: 'badge-danger',
          pulseColor: 'bg-danger-500',
        }
      case 'major':
        return {
          bgColor: 'bg-warning-50',
          borderColor: 'border-warning-200',
          iconColor: 'text-warning-500',
          textColor: 'text-warning-800',
          badgeColor: 'badge-warning',
          pulseColor: 'bg-warning-500',
        }
      case 'minor':
        return {
          bgColor: 'bg-primary-50',
          borderColor: 'border-primary-200',
          iconColor: 'text-primary-500',
          textColor: 'text-primary-800',
          badgeColor: 'badge-info',
          pulseColor: 'bg-primary-500',
        }
    }
  }

  const config = getSeverityConfig(regression.severity)

  const formatMetricName = (metricName: string): string => {
    return metricName
      .replace(/([A-Z])/g, ' $1')
      .replace(/^./, str => str.toUpperCase())
      .trim()
  }

  const formatPercentChange = (percentChange: number): string => {
    const abs = Math.abs(percentChange)
    const sign = percentChange > 0 ? '+' : ''
    return `${sign}${abs.toFixed(1)}%`
  }

  const handleDismiss = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (onDismiss) {
      setIsAnimatingOut(true)
      // Delay the actual dismissal to allow animation
      setTimeout(() => {
        onDismiss(`${regression.runId}-${regression.metricName}`)
      }, 300)
    }
  }

  const handleAcknowledge = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (onAcknowledge) {
      onAcknowledge(`${regression.runId}-${regression.metricName}`)
    }
  }

  const handleViewDetails = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (onViewDetails) {
      onViewDetails(regression)
    }
  }

  const toggleExpanded = () => {
    setIsExpanded(!isExpanded)
  }

  if (dismissed && !isAnimatingOut) {
    return null
  }

  return (
    <div
      className={`
        relative overflow-hidden transition-all duration-300 ease-out
        ${isAnimatingOut ? 'opacity-0 scale-95 -translate-y-2' : 'opacity-100 scale-100 translate-y-0'}
        ${className}
      `}
    >
      <div
        className={`
          border rounded-lg shadow-sm transition-all duration-200
          ${config.bgColor} ${config.borderColor}
          ${acknowledged ? 'opacity-75' : ''}
          hover:shadow-md cursor-pointer
        `}
        onClick={toggleExpanded}
      >
        {/* Alert Header */}
        <div className="p-4">
          <div className="flex items-start justify-between">
            <div className="flex items-start space-x-3">
              {/* Severity Indicator */}
              <div className="flex-shrink-0 relative">
                <ExclamationTriangleIcon className={`h-6 w-6 ${config.iconColor}`} />
                {!acknowledged && regression.severity === 'critical' && (
                  <div className={`absolute -top-1 -right-1 h-3 w-3 rounded-full ${config.pulseColor} animate-pulse`} />
                )}
              </div>

              {/* Alert Content */}
              <div className="flex-1 min-w-0">
                <div className="flex items-center space-x-2 mb-1">
                  <h4 className={`text-sm font-semibold ${config.textColor}`}>
                    Performance Regression Detected
                  </h4>
                  <span className={`badge ${config.badgeColor} text-xs`}>
                    {regression.severity.toUpperCase()}
                  </span>
                  {acknowledged && (
                    <span className="badge badge-success text-xs">
                      <CheckIcon className="h-3 w-3 mr-1" />
                      Acknowledged
                    </span>
                  )}
                </div>

                <p className={`text-sm ${config.textColor} opacity-90`}>
                  <span className="font-medium">{formatMetricName(regression.metricName)}</span>
                  {' '}degraded by{' '}
                  <span className="font-bold">{formatPercentChange(regression.percentChange)}</span>
                  {' '}compared to baseline
                </p>

                {/* Quick Stats */}
                <div className="mt-2 flex items-center space-x-4 text-xs text-gray-600">
                  <span>
                    Baseline: <span className="font-mono">{regression.baselineValue.toFixed(2)}</span>
                  </span>
                  <span>
                    Current: <span className="font-mono">{regression.currentValue.toFixed(2)}</span>
                  </span>
                  <span className="flex items-center">
                    <ClockIcon className="h-3 w-3 mr-1" />
                    Run: <span className="font-mono">{regression.runId.substring(0, 8)}</span>
                  </span>
                </div>
              </div>

              {/* Expand Indicator */}
              <div className="flex-shrink-0">
                <ChevronRightIcon
                  className={`h-5 w-5 text-gray-400 transition-transform duration-200 ${
                    isExpanded ? 'rotate-90' : ''
                  }`}
                />
              </div>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center space-x-1 ml-4">
              {onViewDetails && (
                <button
                  onClick={handleViewDetails}
                  className="p-1 text-gray-400 hover:text-gray-600 transition-colors"
                  title="View detailed analysis"
                >
                  <ArrowTopRightOnSquareIcon className="h-4 w-4" />
                </button>
              )}

              {onAcknowledge && !acknowledged && (
                <button
                  onClick={handleAcknowledge}
                  className="p-1 text-gray-400 hover:text-success-600 transition-colors"
                  title="Acknowledge regression"
                >
                  <CheckIcon className="h-4 w-4" />
                </button>
              )}

              {onDismiss && (
                <button
                  onClick={handleDismiss}
                  className="p-1 text-gray-400 hover:text-gray-600 transition-colors"
                  title="Dismiss alert"
                >
                  <XMarkIcon className="h-4 w-4" />
                </button>
              )}
            </div>
          </div>
        </div>

        {/* Expanded Details */}
        {isExpanded && (
          <div className="border-t border-gray-200 bg-white bg-opacity-50">
            <div className="p-4 space-y-4">
              {/* Detailed Metrics */}
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="space-y-1">
                  <h5 className="text-xs font-semibold text-gray-500 uppercase tracking-wide">
                    Baseline Value
                  </h5>
                  <p className="text-lg font-mono text-gray-900">
                    {regression.baselineValue.toFixed(2)}
                  </p>
                  <p className="text-xs text-gray-500">From baseline run</p>
                </div>

                <div className="space-y-1">
                  <h5 className="text-xs font-semibold text-gray-500 uppercase tracking-wide">
                    Current Value
                  </h5>
                  <p className={`text-lg font-mono ${config.textColor}`}>
                    {regression.currentValue.toFixed(2)}
                  </p>
                  <p className="text-xs text-gray-500">Current measurement</p>
                </div>

                <div className="space-y-1">
                  <h5 className="text-xs font-semibold text-gray-500 uppercase tracking-wide">
                    Change
                  </h5>
                  <p className={`text-lg font-mono font-bold ${config.textColor}`}>
                    {formatPercentChange(regression.percentChange)}
                  </p>
                  <p className="text-xs text-gray-500">
                    Δ {(regression.currentValue - regression.baselineValue).toFixed(2)}
                  </p>
                </div>
              </div>

              {/* Impact Assessment */}
              <div className="bg-gray-50 rounded-lg p-3">
                <h5 className="text-sm font-semibold text-gray-700 mb-2">Impact Assessment</h5>
                <div className="space-y-2 text-sm text-gray-600">
                  <div className="flex justify-between">
                    <span>Severity Level:</span>
                    <span className={`font-medium ${config.textColor}`}>
                      {regression.severity.charAt(0).toUpperCase() + regression.severity.slice(1)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span>Metric Category:</span>
                    <span className="font-medium">
                      {regression.metricName.includes('latency') ? 'Latency' :
                       regression.metricName.includes('throughput') ? 'Throughput' :
                       regression.metricName.includes('success') ? 'Reliability' : 'Performance'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span>Affected Run:</span>
                    <span className="font-mono text-xs">{regression.runId}</span>
                  </div>
                  <div className="flex justify-between">
                    <span>Baseline Run:</span>
                    <span className="font-mono text-xs">{regression.baselineId}</span>
                  </div>
                </div>
              </div>

              {/* Recommended Actions */}
              <div className="bg-blue-50 rounded-lg p-3">
                <h5 className="text-sm font-semibold text-blue-700 mb-2">Recommended Actions</h5>
                <ul className="space-y-1 text-sm text-blue-600">
                  {regression.severity === 'critical' && (
                    <>
                      <li>• Investigate immediately - this may impact production</li>
                      <li>• Review recent code changes and deployments</li>
                      <li>• Consider rolling back recent changes</li>
                    </>
                  )}
                  {regression.severity === 'major' && (
                    <>
                      <li>• Schedule investigation within 24 hours</li>
                      <li>• Review performance-related commits</li>
                      <li>• Run additional tests to confirm regression</li>
                    </>
                  )}
                  {regression.severity === 'minor' && (
                    <>
                      <li>• Monitor trend in subsequent runs</li>
                      <li>• Consider performance optimization opportunities</li>
                      <li>• Document for next maintenance cycle</li>
                    </>
                  )}
                  <li>• Compare with similar historical patterns</li>
                  <li>• Update performance baselines if needed</li>
                </ul>
              </div>

              {/* Action Buttons */}
              <div className="flex justify-end space-x-2 pt-2">
                {onViewDetails && (
                  <button
                    onClick={handleViewDetails}
                    className="btn btn-outline btn-sm"
                  >
                    <ArrowTopRightOnSquareIcon className="h-4 w-4 mr-1" />
                    View Full Analysis
                  </button>
                )}

                {onAcknowledge && !acknowledged && (
                  <button
                    onClick={handleAcknowledge}
                    className="btn btn-primary btn-sm"
                  >
                    <CheckIcon className="h-4 w-4 mr-1" />
                    Acknowledge
                  </button>
                )}

                {onDismiss && (
                  <button
                    onClick={handleDismiss}
                    className="btn btn-secondary btn-sm"
                  >
                    Dismiss
                  </button>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * RegressionAlertList component for displaying multiple regression alerts
 */
export interface RegressionAlertListProps {
  regressions: Regression[]
  onDismiss?: (regressionId: string) => void
  onAcknowledge?: (regressionId: string) => void
  onViewDetails?: (regression: Regression) => void
  dismissedAlerts?: Set<string>
  acknowledgedAlerts?: Set<string>
  maxVisible?: number
  className?: string
}

export function RegressionAlertList({
  regressions,
  onDismiss,
  onAcknowledge,
  onViewDetails,
  dismissedAlerts = new Set(),
  acknowledgedAlerts = new Set(),
  maxVisible = 5,
  className = '',
}: RegressionAlertListProps) {
  const [showAll, setShowAll] = useState(false)

  // Sort by severity and then by percent change
  const sortedRegressions = [...regressions].sort((a, b) => {
    const severityOrder = { critical: 3, major: 2, minor: 1 }
    const severityDiff = severityOrder[b.severity] - severityOrder[a.severity]
    if (severityDiff !== 0) return severityDiff
    return Math.abs(b.percentChange) - Math.abs(a.percentChange)
  })

  const visibleRegressions = showAll 
    ? sortedRegressions 
    : sortedRegressions.slice(0, maxVisible)

  const hiddenCount = sortedRegressions.length - maxVisible

  if (regressions.length === 0) {
    return (
      <div className={`text-center py-8 ${className}`}>
        <CheckIcon className="h-12 w-12 text-success-500 mx-auto mb-3" />
        <h3 className="text-lg font-medium text-gray-900 mb-1">
          No Active Regressions
        </h3>
        <p className="text-gray-500">
          All performance metrics are within expected ranges.
        </p>
      </div>
    )
  }

  return (
    <div className={`space-y-3 ${className}`}>
      {visibleRegressions.map((regression) => {
        const alertId = `${regression.runId}-${regression.metricName}`
        return (
          <RegressionAlert
            key={alertId}
            regression={regression}
            onDismiss={onDismiss}
            onAcknowledge={onAcknowledge}
            onViewDetails={onViewDetails}
            dismissed={dismissedAlerts.has(alertId)}
            acknowledged={acknowledgedAlerts.has(alertId)}
          />
        )
      })}

      {!showAll && hiddenCount > 0 && (
        <button
          onClick={() => setShowAll(true)}
          className="w-full p-3 text-center text-sm text-gray-500 hover:text-gray-700 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors"
        >
          Show {hiddenCount} more regression{hiddenCount > 1 ? 's' : ''}
        </button>
      )}

      {showAll && hiddenCount > 0 && (
        <button
          onClick={() => setShowAll(false)}
          className="w-full p-3 text-center text-sm text-gray-500 hover:text-gray-700 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors"
        >
          Show fewer
        </button>
      )}
    </div>
  )
}


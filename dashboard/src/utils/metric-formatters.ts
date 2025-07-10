// Comprehensive metric formatting utilities for the dashboard
// Provides consistent formatting across all metric displays

/**
 * Format latency value in milliseconds to human-readable string
 * Limited to 2 decimal places for better readability
 * @param ms - Latency in milliseconds
 * @returns Formatted latency string with appropriate unit
 */
export function formatLatency(ms: number): string {
  if (ms === 0) return '0ms'
  if (ms < 0.01) return '<0.01ms'
  if (ms < 1) return `${Number(ms.toFixed(2))}ms`
  if (ms < 10) return `${Number(ms.toFixed(2))}ms`
  if (ms < 100) return `${Number(ms.toFixed(1))}ms`
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 10000) return `${Number((ms / 1000).toFixed(2))}s`
  if (ms < 60000) return `${Number((ms / 1000).toFixed(1))}s`
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ${Math.round((ms % 60000) / 1000)}s`
  return `${Math.floor(ms / 3600000)}h ${Math.floor((ms % 3600000) / 60000)}m`
}

/**
 * Format latency value handling null/undefined as N/A
 * @param ms - Latency in milliseconds (nullable)
 * @returns Formatted latency string or "N/A" for null/undefined values
 */
export function formatLatencyValue(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || isNaN(ms) || !isFinite(ms)) {
    return 'N/A'
  }
  return formatLatency(ms)
}

/**
 * Check if a value is null/undefined/NaN
 * @param value - Value to check
 * @returns true if the value is null/undefined/NaN
 */
export function isNullOrUndefined(value: any): boolean {
  return value === null || value === undefined || (typeof value === 'number' && (isNaN(value) || !isFinite(value)))
}

/**
 * Format percentage value with consistent decimal places
 * Limited to 2 decimal places for better readability
 * @param value - Percentage value (0-100)
 * @returns Formatted percentage string
 */
export function formatPercentage(value: number): string {
  if (value === 0) return '0%'
  if (value === 100) return '100%'
  if (value < 0.01) return '<0.01%'
  if (value >= 99.995) return '100%' // Round very close to 100
  return `${Number(value.toFixed(2))}%`
}

/**
 * Format percentage value handling null/undefined as N/A
 * @param value - Percentage value (0-100) (nullable)
 * @returns Formatted percentage string or "N/A" for null/undefined values
 */
export function formatPercentageValue(value: number | null | undefined): string {
  if (isNullOrUndefined(value)) {
    return 'N/A'
  }
  return formatPercentage(value)
}

/**
 * Format throughput in requests per second
 * Limited to 3 significant digits for consistency
 * @param rps - Requests per second
 * @returns Formatted throughput string
 */
export function formatThroughput(rps: number): string {
  // Handle invalid values
  if (!isFinite(rps) || isNaN(rps) || rps < 0) {
    return 'N/A'
  }
  if (rps === 0) return '0 rps'
  if (rps < 0.001) return '<0.001 rps'
  if (rps < 1) return `${Number(rps.toPrecision(3))} rps`
  if (rps < 10) return `${Number(rps.toPrecision(3))} rps`
  if (rps < 100) return `${Number(rps.toPrecision(3))} rps`
  if (rps < 1000) return `${Math.round(rps)} rps`
  if (rps < 1000000) return `${Number((rps / 1000).toPrecision(3))}k rps`
  return `${Number((rps / 1000000).toPrecision(3))}M rps`
}

/**
 * Format throughput value handling null/undefined as N/A
 * @param rps - Requests per second (nullable)
 * @returns Formatted throughput string or "N/A" for null/undefined values
 */
export function formatThroughputValue(rps: number | null | undefined): string {
  if (isNullOrUndefined(rps)) {
    return 'N/A'
  }
  return formatThroughput(rps)
}

/**
 * Format bytes to human-readable format
 * @param bytes - Number of bytes
 * @returns Formatted bytes string with appropriate unit
 */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  if (bytes < 0) return '-' + formatBytes(-bytes)
  
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const base = 1024
  
  let unitIndex = 0
  let value = bytes
  
  while (value >= base && unitIndex < units.length - 1) {
    value /= base
    unitIndex++
  }
  
  if (unitIndex === 0) return `${Math.round(value)} ${units[unitIndex]}`
  if (value < 10) return `${value.toFixed(1)} ${units[unitIndex]}`
  return `${Math.round(value)} ${units[unitIndex]}`
}

/**
 * Format duration in seconds to human-readable format
 * @param seconds - Duration in seconds
 * @returns Formatted duration string
 */
export function formatDuration(seconds: number): string {
  if (seconds === 0) return '0s'
  if (seconds < 0) return '-' + formatDuration(-seconds)
  if (seconds < 1) return `${(seconds * 1000).toFixed(0)}ms`
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  if (seconds < 3600) {
    const minutes = Math.floor(seconds / 60)
    const remainingSeconds = Math.round(seconds % 60)
    return remainingSeconds > 0 ? `${minutes}m ${remainingSeconds}s` : `${minutes}m`
  }
  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600)
    const remainingMinutes = Math.round((seconds % 3600) / 60)
    return remainingMinutes > 0 ? `${hours}h ${remainingMinutes}m` : `${hours}h`
  }
  const days = Math.floor(seconds / 86400)
  const remainingHours = Math.round((seconds % 86400) / 3600)
  return remainingHours > 0 ? `${days}d ${remainingHours}h` : `${days}d`
}

/**
 * Get color class based on latency value
 * @param latency - Latency in milliseconds
 * @returns CSS color class string
 */
export function getLatencyColor(latency: number): string {
  if (latency < 50) return 'text-green-600'
  if (latency < 100) return 'text-yellow-600'
  if (latency < 200) return 'text-orange-600'
  if (latency < 500) return 'text-red-600'
  return 'text-red-800'
}

/**
 * Get color class based on success rate
 * @param rate - Success rate percentage (0-100)
 * @returns CSS color class string
 */
export function getSuccessRateColor(rate: number): string {
  if (rate >= 99.9) return 'text-green-600'
  if (rate >= 99) return 'text-green-500'
  if (rate >= 95) return 'text-yellow-600'
  if (rate >= 90) return 'text-orange-600'
  if (rate >= 80) return 'text-red-600'
  return 'text-red-800'
}

/**
 * Format percentile value with consistent formatting
 * @param percentile - Percentile value (e.g., 95 for P95)
 * @returns Formatted percentile string
 */
export function formatPercentile(percentile: number): string {
  if (percentile === 50) return 'P50'
  if (percentile === 75) return 'P75'
  if (percentile === 90) return 'P90'
  if (percentile === 95) return 'P95'
  if (percentile === 99) return 'P99'
  if (percentile === 99.9) return 'P99.9'
  if (percentile === 99.99) return 'P99.99'
  if (percentile % 1 === 0) return `P${Math.round(percentile)}`
  return `P${percentile.toFixed(1)}`
}

/**
 * Format error count with appropriate scaling
 * @param count - Error count
 * @returns Formatted error count string
 */
export function formatErrorCount(count: number): string {
  if (count === 0) return '0'
  if (count < 1000) return count.toString()
  if (count < 1000000) return `${(count / 1000).toFixed(1)}k`
  return `${(count / 1000000).toFixed(1)}M`
}

/**
 * Format request count with appropriate scaling
 * @param count - Request count
 * @returns Formatted request count string
 */
export function formatRequestCount(count: number): string {
  if (count === 0) return '0'
  if (count < 1000) return count.toString()
  if (count < 1000000) return `${(count / 1000).toFixed(1)}k`
  if (count < 1000000000) return `${(count / 1000000).toFixed(1)}M`
  return `${(count / 1000000000).toFixed(1)}B`
}


/**
 * Format memory usage with appropriate unit
 * @param bytes - Memory in bytes
 * @returns Formatted memory string
 */
export function formatMemory(bytes: number): string {
  return formatBytes(bytes)
}

/**
 * Format CPU usage percentage
 * @param percentage - CPU usage percentage (0-100)
 * @returns Formatted CPU usage string
 */
export function formatCPUUsage(percentage: number): string {
  return `${percentage.toFixed(1)}%`
}

/**
 * Format performance score (0-100)
 * @param score - Performance score (0-100)
 * @returns Formatted performance score string
 */
export function formatPerformanceScore(score: number): string {
  if (isNullOrUndefined(score)) {
    return 'N/A'
  }
  return `${Math.round(score)}/100`
}

/**
 * Format network speed/bandwidth
 * @param bytesPerSecond - Bytes per second
 * @returns Formatted network speed string
 */
export function formatNetworkSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) return '0 B/s'
  if (bytesPerSecond < 1024) return `${Math.round(bytesPerSecond)} B/s`
  if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`
  if (bytesPerSecond < 1024 * 1024 * 1024) return `${(bytesPerSecond / (1024 * 1024)).toFixed(1)} MB/s`
  return `${(bytesPerSecond / (1024 * 1024 * 1024)).toFixed(1)} GB/s`
}

/**
 * Format number with appropriate scaling and precision
 * @param value - Numeric value
 * @param unit - Unit suffix (optional)
 * @returns Formatted number string
 */
export function formatNumber(value: number, unit?: string): string {
  const suffix = unit ? ` ${unit}` : ''
  
  if (value === 0) return `0${suffix}`
  if (Math.abs(value) < 0.01) return `<0.01${suffix}`
  if (Math.abs(value) < 1) return `${value.toFixed(2)}${suffix}`
  if (Math.abs(value) < 10) return `${value.toFixed(1)}${suffix}`
  if (Math.abs(value) < 1000) return `${Math.round(value)}${suffix}`
  if (Math.abs(value) < 1000000) return `${(value / 1000).toFixed(1)}k${suffix}`
  return `${(value / 1000000).toFixed(1)}M${suffix}`
}

/**
 * Format time in milliseconds to relative time string
 * @param timestamp - Timestamp in milliseconds
 * @returns Relative time string (e.g., "2 minutes ago")
 */
export function formatRelativeTime(timestamp: number): string {
  const now = Date.now()
  const diffMs = now - timestamp
  const diffSeconds = Math.floor(diffMs / 1000)
  const diffMinutes = Math.floor(diffSeconds / 60)
  const diffHours = Math.floor(diffMinutes / 60)
  const diffDays = Math.floor(diffHours / 24)
  
  if (diffSeconds < 60) return 'just now'
  if (diffMinutes < 60) return `${diffMinutes} minute${diffMinutes !== 1 ? 's' : ''} ago`
  if (diffHours < 24) return `${diffHours} hour${diffHours !== 1 ? 's' : ''} ago`
  if (diffDays < 7) return `${diffDays} day${diffDays !== 1 ? 's' : ''} ago`
  if (diffDays < 30) return `${Math.floor(diffDays / 7)} week${Math.floor(diffDays / 7) !== 1 ? 's' : ''} ago`
  return `${Math.floor(diffDays / 30)} month${Math.floor(diffDays / 30) !== 1 ? 's' : ''} ago`
}

/**
 * Format change in percentage (delta)
 * @param current - Current value
 * @param previous - Previous value
 * @returns Object with formatted delta and color
 */
export function formatDelta(current: number, previous: number): { value: string; color: string; isPositive: boolean } {
  if (previous === 0) return { value: 'N/A', color: 'text-gray-600', isPositive: false }
  
  const delta = ((current - previous) / previous) * 100
  const isPositive = delta > 0
  const absValue = Math.abs(delta)
  
  let formattedValue: string
  if (absValue < 0.01) formattedValue = '<0.01%'
  else if (absValue < 0.1) formattedValue = `${absValue.toFixed(2)}%`
  else if (absValue < 1) formattedValue = `${absValue.toFixed(1)}%`
  else formattedValue = `${Math.round(absValue)}%`
  
  const prefix = isPositive ? '+' : '-'
  const color = isPositive ? 'text-green-600' : 'text-red-600'
  
  return {
    value: `${prefix}${formattedValue}`,
    color,
    isPositive
  }
}

/**
 * Format trend indicator based on direction and confidence
 * @param direction - Trend direction
 * @param confidence - Confidence level (0-100)
 * @returns Object with symbol, color, and description
 */
export function formatTrend(direction: 'improving' | 'degrading' | 'stable', confidence: number): {
  symbol: string
  color: string
  description: string
} {
  const confidenceLevel = confidence >= 80 ? 'high' : confidence >= 60 ? 'medium' : 'low'
  
  switch (direction) {
    case 'improving':
      return {
        symbol: '↗',
        color: 'text-green-600',
        description: `Improving (${confidenceLevel} confidence)`
      }
    case 'degrading':
      return {
        symbol: '↘',
        color: 'text-red-600',
        description: `Degrading (${confidenceLevel} confidence)`
      }
    case 'stable':
      return {
        symbol: '→',
        color: 'text-gray-600',
        description: `Stable (${confidenceLevel} confidence)`
      }
    default:
      return {
        symbol: '?',
        color: 'text-gray-400',
        description: 'Unknown trend'
      }
  }
}

/**
 * Format severity level with appropriate color
 * @param severity - Severity level
 * @returns Object with formatted severity and color
 */
export function formatSeverity(severity: 'low' | 'medium' | 'high' | 'critical'): {
  label: string
  color: string
  bgColor: string
} {
  switch (severity) {
    case 'low':
      return {
        label: 'Low',
        color: 'text-green-700',
        bgColor: 'bg-green-100'
      }
    case 'medium':
      return {
        label: 'Medium',
        color: 'text-yellow-700',
        bgColor: 'bg-yellow-100'
      }
    case 'high':
      return {
        label: 'High',
        color: 'text-orange-700',
        bgColor: 'bg-orange-100'
      }
    case 'critical':
      return {
        label: 'Critical',
        color: 'text-red-700',
        bgColor: 'bg-red-100'
      }
    default:
      return {
        label: 'Unknown',
        color: 'text-gray-700',
        bgColor: 'bg-gray-100'
      }
  }
}

/**
 * Format uptime percentage
 * @param uptime - Uptime percentage (0-100)
 * @returns Formatted uptime string with color
 */
export function formatUptime(uptime: number): { value: string; color: string } {
  const formatted = formatPercentage(uptime)
  let color: string
  
  if (uptime >= 99.9) color = 'text-green-600'
  else if (uptime >= 99) color = 'text-green-500'
  else if (uptime >= 95) color = 'text-yellow-600'
  else if (uptime >= 90) color = 'text-orange-600'
  else color = 'text-red-600'
  
  return { value: formatted, color }
}

/**
 * Format connection count with scaling
 * @param count - Connection count
 * @returns Formatted connection count string
 */
export function formatConnectionCount(count: number): string {
  if (count === 0) return '0'
  if (count < 1000) return count.toString()
  if (count < 1000000) return `${(count / 1000).toFixed(1)}k`
  return `${(count / 1000000).toFixed(1)}M`
}

/**
 * Format file size with appropriate unit
 * @param bytes - File size in bytes
 * @returns Formatted file size string
 */
export function formatFileSize(bytes: number): string {
  return formatBytes(bytes)
}

/**
 * Format frequency (Hz) with appropriate unit
 * @param hz - Frequency in Hz
 * @returns Formatted frequency string
 */
export function formatFrequency(hz: number): string {
  if (hz === 0) return '0 Hz'
  if (hz < 1000) return `${hz.toFixed(1)} Hz`
  if (hz < 1000000) return `${(hz / 1000).toFixed(1)} kHz`
  if (hz < 1000000000) return `${(hz / 1000000).toFixed(1)} MHz`
  return `${(hz / 1000000000).toFixed(1)} GHz`
}

/**
 * Format temperature with unit
 * @param celsius - Temperature in Celsius
 * @returns Formatted temperature string
 */
export function formatTemperature(celsius: number): string {
  return `${celsius.toFixed(1)}°C`
}

/**
 * Format voltage with unit
 * @param volts - Voltage in volts
 * @returns Formatted voltage string
 */
export function formatVoltage(volts: number): string {
  if (volts < 1) return `${(volts * 1000).toFixed(0)}mV`
  return `${volts.toFixed(2)}V`
}

/**
 * Format timestamp to human-readable date/time
 * @param timestamp - Timestamp (Date object or ISO string)
 * @returns Formatted date/time string
 */
export function formatTimestamp(timestamp: Date | string): string {
  const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp
  return date.toLocaleString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

/**
 * Format version string with proper formatting
 * @param version - Version string
 * @returns Formatted version string
 */
export function formatVersion(version: string): string {
  if (!version) return 'Unknown'
  // Remove 'v' prefix if present
  return version.startsWith('v') ? version.slice(1) : version
}

/**
 * Format boolean value to Yes/No
 * @param value - Boolean value
 * @returns "Yes" or "No"
 */
export function formatBoolean(value: boolean): string {
  return value ? 'Yes' : 'No'
}

/**
 * Format array of strings to comma-separated list
 * @param items - Array of strings
 * @param maxItems - Maximum items to show before truncating
 * @returns Formatted string list
 */
export function formatList(items: string[], maxItems: number = 3): string {
  if (items.length === 0) return 'None'
  if (items.length <= maxItems) return items.join(', ')
  return `${items.slice(0, maxItems).join(', ')} +${items.length - maxItems} more`
}

/**
 * Format statistical confidence interval
 * @param lower - Lower bound
 * @param upper - Upper bound
 * @param confidence - Confidence level (0-100)
 * @returns Formatted confidence interval string
 */
export function formatConfidenceInterval(lower: number, upper: number, confidence: number): string {
  return `${lower.toFixed(2)} - ${upper.toFixed(2)} (${confidence}% CI)`
}

/**
 * Format p-value for statistical significance
 * @param pValue - P-value
 * @returns Formatted p-value string
 */
export function formatPValue(pValue: number): string {
  if (pValue < 0.001) return 'p < 0.001'
  if (pValue < 0.01) return `p = ${pValue.toFixed(3)}`
  if (pValue < 0.05) return `p = ${pValue.toFixed(3)}`
  return `p = ${pValue.toFixed(2)}`
}

/**
 * Format standard deviation with appropriate precision
 * @param value - Standard deviation value
 * @param unit - Unit suffix (optional)
 * @returns Formatted standard deviation string
 */
export function formatStandardDeviation(value: number, unit?: string): string {
  const suffix = unit ? ` ${unit}` : ''
  return `±${value.toFixed(2)}${suffix}`
}

/**
 * Format correlation coefficient
 * @param correlation - Correlation coefficient (-1 to 1)
 * @returns Formatted correlation string with interpretation
 */
export function formatCorrelation(correlation: number): {
  value: string
  strength: string
  color: string
} {
  const abs = Math.abs(correlation)
  const value = correlation.toFixed(3)
  
  let strength: string
  let color: string
  
  if (abs >= 0.8) {
    strength = 'Strong'
    color = 'text-green-600'
  } else if (abs >= 0.6) {
    strength = 'Moderate'
    color = 'text-yellow-600'
  } else if (abs >= 0.4) {
    strength = 'Weak'
    color = 'text-orange-600'
  } else {
    strength = 'Very Weak'
    color = 'text-red-600'
  }
  
  return { value, strength, color }
}

/**
 * Format regression R-squared value
 * @param rSquared - R-squared value (0-1)
 * @returns Formatted R-squared string
 */
export function formatRSquared(rSquared: number): string {
  const percentage = (rSquared * 100).toFixed(1)
  return `R² = ${rSquared.toFixed(3)} (${percentage}%)`
}

/**
 * Format range with min and max values
 * @param min - Minimum value
 * @param max - Maximum value
 * @param unit - Unit suffix (optional)
 * @returns Formatted range string
 */
export function formatRange(min: number, max: number, unit?: string): string {
  const suffix = unit ? ` ${unit}` : ''
  return `${min.toFixed(2)} - ${max.toFixed(2)}${suffix}`
}
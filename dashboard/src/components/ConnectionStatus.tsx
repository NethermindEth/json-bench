import { useEffect } from 'react'
import { useHealthCheck } from '../api/hooks'

interface ConnectionStatusProps {
  className?: string
}

export default function ConnectionStatus({ className = '' }: ConnectionStatusProps) {
  const { data, error, isLoading, isError } = useHealthCheck()

  // Log connection status changes
  useEffect(() => {
    if (!isLoading && !isError && data) {
      console.log('🟢 Connection Status: Connected to API', {
        status: data.status,
        version: data.version,
        services: data.services
      })
    } else if (!isLoading && isError) {
      console.log('🔴 Connection Status: Disconnected from API', {
        error: error?.message,
        details: error
      })
    } else if (isLoading) {
      console.log('🟡 Connection Status: Checking connection...')
    }
  }, [data, error, isLoading, isError])

  // Determine status
  const getStatusInfo = () => {
    if (isLoading) {
      return {
        status: 'checking',
        color: 'bg-yellow-500',
        text: 'Checking...',
        animate: 'animate-pulse'
      }
    }
    
    if (isError || !data) {
      // Provide more detailed error information
      const errorMsg = error?.message || 'Connection failed'
      return {
        status: 'disconnected',
        color: 'bg-red-500',
        text: 'Disconnected',
        animate: '',
        errorDetails: errorMsg
      }
    }
    
    // Check for detailed health status
    if (data.status === 'healthy') {
      return {
        status: 'connected',
        color: 'bg-green-500',
        text: 'Connected',
        animate: '',
        details: `API v${data.version || 'unknown'}`
      }
    }
    
    if (data.status === 'unhealthy') {
      return {
        status: 'degraded',
        color: 'bg-orange-500',
        text: 'Degraded',
        animate: 'animate-pulse',
        errorDetails: 'Some services unavailable'
      }
    }
    
    return {
      status: 'unknown',
      color: 'bg-gray-500',
      text: 'Unknown',
      animate: '',
      errorDetails: `Unexpected status: ${data.status}`
    }
  }

  const statusInfo = getStatusInfo()
  
  return (
    <div className={`flex items-center space-x-2 ${className}`}>
      <div 
        className={`h-2 w-2 rounded-full ${statusInfo.color} ${statusInfo.animate}`}
        aria-hidden="true"
      />
      <span 
        className="text-sm text-gray-500"
        aria-label={`API connection status: ${statusInfo.text}`}
      >
        {statusInfo.text}
      </span>
      {statusInfo.details && (
        <span className="text-xs text-gray-400 ml-1">
          ({statusInfo.details})
        </span>
      )}
      {statusInfo.errorDetails && (
        <span 
          className="text-xs text-red-600 ml-1 cursor-help"
          title={statusInfo.errorDetails}
        >
          ⚠️
        </span>
      )}
    </div>
  )
}
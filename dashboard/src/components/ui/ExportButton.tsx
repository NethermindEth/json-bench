import React, { useState, useRef } from 'react'
import {
  DocumentArrowDownIcon,
  ChevronDownIcon,
  DocumentTextIcon,
  TableCellsIcon,
  DocumentIcon,
  CheckCircleIcon,
  ExclamationCircleIcon,
} from '@heroicons/react/24/outline'
import LoadingSpinner from '../LoadingSpinner'

export type ExportFormat = 'json' | 'csv' | 'xlsx' | 'txt'

export interface ExportButtonProps {
  data: any
  filename: string
  formats?: ExportFormat[]
  onExport?: (format: ExportFormat, data: any) => void | Promise<void>
  className?: string
  variant?: 'primary' | 'secondary' | 'outline'
  size?: 'sm' | 'md' | 'lg'
  disabled?: boolean
  showProgress?: boolean
  customFormatters?: Partial<Record<ExportFormat, (data: any) => string | Blob>>
}

interface ExportProgress {
  format: ExportFormat
  status: 'preparing' | 'exporting' | 'success' | 'error'
  progress: number
  message?: string
}

const formatIcons = {
  json: DocumentIcon,
  csv: TableCellsIcon,
  xlsx: TableCellsIcon,
  txt: DocumentTextIcon,
}

const formatLabels = {
  json: 'JSON',
  csv: 'CSV',
  xlsx: 'Excel',
  txt: 'Text',
}

const formatMimeTypes = {
  json: 'application/json',
  csv: 'text/csv',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  txt: 'text/plain',
}

// Helper function to convert data to CSV
const convertToCSV = (data: any[]): string => {
  if (!Array.isArray(data) || data.length === 0) {
    return ''
  }

  // Get headers from first object
  const headers = Object.keys(data[0])
  const csvRows = []

  // Add headers
  csvRows.push(headers.join(','))

  // Add data rows
  for (const row of data) {
    const values = headers.map(header => {
      const value = row[header]
      // Handle special characters and quotes
      if (typeof value === 'string') {
        return `"${value.replace(/"/g, '""')}"`
      }
      return value?.toString() || ''
    })
    csvRows.push(values.join(','))
  }

  return csvRows.join('\n')
}

// Helper function to convert data to JSON
const convertToJSON = (data: any): string => {
  return JSON.stringify(data, null, 2)
}

// Helper function to convert data to text
const convertToText = (data: any): string => {
  if (Array.isArray(data)) {
    return data.map(item => 
      typeof item === 'object' ? JSON.stringify(item, null, 2) : String(item)
    ).join('\n\n')
  }
  return typeof data === 'object' ? JSON.stringify(data, null, 2) : String(data)
}

// Helper function to download file
const downloadFile = (content: string | Blob, filename: string, mimeType: string) => {
  const blob = content instanceof Blob ? content : new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

export function ExportButton({
  data,
  filename,
  formats = ['json', 'csv', 'xlsx'],
  onExport,
  className = '',
  variant = 'outline',
  size = 'md',
  disabled = false,
  showProgress = true,
  customFormatters = {},
}: ExportButtonProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [exportProgress, setExportProgress] = useState<ExportProgress | null>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  const getButtonClasses = () => {
    const baseClasses = 'btn relative'
    const sizeClasses = {
      sm: 'px-3 py-1.5 text-xs',
      md: 'px-4 py-2 text-sm',
      lg: 'px-6 py-3 text-base',
    }
    const variantClasses = {
      primary: 'btn-primary',
      secondary: 'btn-secondary',
      outline: 'btn-outline',
    }
    
    return `${baseClasses} ${sizeClasses[size]} ${variantClasses[variant]} ${className}`
  }

  const handleExport = async (format: ExportFormat) => {
    if (disabled) return

    setIsOpen(false)
    
    if (showProgress) {
      setExportProgress({
        format,
        status: 'preparing',
        progress: 0,
        message: 'Preparing export...',
      })
    }

    try {
      // Custom export handler
      if (onExport) {
        if (showProgress) {
          setExportProgress(prev => prev ? { ...prev, status: 'exporting', progress: 50 } : null)
        }
        await onExport(format, data)
        if (showProgress) {
          setExportProgress(prev => prev ? { ...prev, status: 'success', progress: 100 } : null)
        }
        return
      }

      // Built-in export logic
      let content: string | Blob
      let fileExtension: string
      let mimeType: string

      if (showProgress) {
        setExportProgress(prev => prev ? { ...prev, status: 'exporting', progress: 30 } : null)
      }

      // Use custom formatter if provided
      if (customFormatters[format]) {
        const result = customFormatters[format]!(data)
        content = result
        fileExtension = format
        mimeType = formatMimeTypes[format]
      } else {
        // Built-in formatters
        switch (format) {
          case 'json':
            content = convertToJSON(data)
            fileExtension = 'json'
            mimeType = formatMimeTypes.json
            break
          case 'csv':
            content = convertToCSV(Array.isArray(data) ? data : [data])
            fileExtension = 'csv'
            mimeType = formatMimeTypes.csv
            break
          case 'txt':
            content = convertToText(data)
            fileExtension = 'txt'
            mimeType = formatMimeTypes.txt
            break
          case 'xlsx':
            // For Excel, we'll fall back to CSV format
            // In a real implementation, you'd use a library like xlsx
            content = convertToCSV(Array.isArray(data) ? data : [data])
            fileExtension = 'csv'
            mimeType = formatMimeTypes.csv
            break
          default:
            throw new Error(`Unsupported format: ${format}`)
        }
      }

      if (showProgress) {
        setExportProgress(prev => prev ? { ...prev, progress: 80 } : null)
      }

      // Download the file
      const finalFilename = `${filename}.${fileExtension}`
      downloadFile(content, finalFilename, mimeType)

      if (showProgress) {
        setExportProgress(prev => prev ? { ...prev, status: 'success', progress: 100 } : null)
      }
    } catch (error) {
      console.error('Export failed:', error)
      if (showProgress) {
        setExportProgress({
          format,
          status: 'error',
          progress: 0,
          message: error instanceof Error ? error.message : 'Export failed',
        })
      }
    }

    // Clear progress after delay
    if (showProgress) {
      setTimeout(() => setExportProgress(null), 3000)
    }
  }

  // Close dropdown when clicking outside
  React.useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Single format - simple button
  if (formats.length === 1) {
    const format = formats[0]
    const Icon = formatIcons[format]
    
    return (
      <button
        onClick={() => handleExport(format)}
        disabled={disabled || !!exportProgress}
        className={getButtonClasses()}
      >
        {exportProgress ? (
          <div className="flex items-center">
            <LoadingSpinner size="sm" />
            <span className="ml-2">{exportProgress.message}</span>
          </div>
        ) : (
          <div className="flex items-center">
            <Icon className="h-4 w-4 mr-2" />
            Export {formatLabels[format]}
          </div>
        )}
      </button>
    )
  }

  // Multiple formats - dropdown
  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        disabled={disabled || !!exportProgress}
        className={getButtonClasses()}
      >
        {exportProgress ? (
          <div className="flex items-center">
            {exportProgress.status === 'success' ? (
              <CheckCircleIcon className="h-4 w-4 mr-2 text-success-600" />
            ) : exportProgress.status === 'error' ? (
              <ExclamationCircleIcon className="h-4 w-4 mr-2 text-danger-600" />
            ) : (
              <LoadingSpinner size="sm" />
            )}
            <span className="ml-2">{exportProgress.message || 'Exporting...'}</span>
          </div>
        ) : (
          <div className="flex items-center">
            <DocumentArrowDownIcon className="h-4 w-4 mr-2" />
            Export
            <ChevronDownIcon className="h-4 w-4 ml-2" />
          </div>
        )}
      </button>

      {isOpen && !exportProgress && (
        <div className="absolute right-0 mt-2 w-48 bg-white rounded-md shadow-lg ring-1 ring-black ring-opacity-5 z-50">
          <div className="py-1">
            {formats.map((format) => {
              const Icon = formatIcons[format]
              return (
                <button
                  key={format}
                  onClick={() => handleExport(format)}
                  className="flex items-center w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900"
                >
                  <Icon className="h-4 w-4 mr-3" />
                  {formatLabels[format]}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Progress indicator */}
      {exportProgress && showProgress && (
        <div className="absolute top-full left-0 right-0 mt-2 p-3 bg-white rounded-md shadow-lg ring-1 ring-black ring-opacity-5 z-50">
          <div className="flex items-center justify-between mb-2">
            <span className="text-sm font-medium">
              Exporting {formatLabels[exportProgress.format]}
            </span>
            <span className="text-sm text-gray-500">
              {exportProgress.progress}%
            </span>
          </div>
          <div className="w-full bg-gray-200 rounded-full h-2">
            <div
              className={`h-2 rounded-full transition-all duration-300 ${
                exportProgress.status === 'success' ? 'bg-success-600' :
                exportProgress.status === 'error' ? 'bg-danger-600' :
                'bg-primary-600'
              }`}
              style={{ width: `${exportProgress.progress}%` }}
            />
          </div>
          {exportProgress.message && (
            <p className="text-xs text-gray-600 mt-1">{exportProgress.message}</p>
          )}
        </div>
      )}
    </div>
  )
}

export default ExportButton
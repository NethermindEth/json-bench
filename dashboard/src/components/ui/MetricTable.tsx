import React, { useState, useMemo, useRef, useEffect } from 'react'
import {
  ChevronDownIcon,
  ChevronUpIcon,
  FunnelIcon,
  MagnifyingGlassIcon,
  XMarkIcon,
  DocumentArrowDownIcon,
  ArrowPathIcon,
} from '@heroicons/react/24/outline'
import LoadingSpinner from '../LoadingSpinner'
import ExportButton from './ExportButton'

export interface ColumnDef<T> {
  id: string
  header: string
  accessor: keyof T | ((row: T) => any)
  sortable?: boolean
  filterable?: boolean
  width?: string
  minWidth?: string
  maxWidth?: string
  render?: (value: any, row: T) => React.ReactNode
  className?: string
  headerClassName?: string
  filterType?: 'text' | 'number' | 'select' | 'date'
  filterOptions?: Array<{ label: string; value: string }>
  sortFn?: (a: T, b: T) => number
}

export interface MetricTableProps<T> {
  data: T[]
  columns: ColumnDef<T>[]
  sortable?: boolean
  filterable?: boolean
  exportable?: boolean
  loading?: boolean
  emptyMessage?: string
  className?: string
  pageSize?: number
  maxHeight?: string
  stickyHeader?: boolean
  striped?: boolean
  bordered?: boolean
  onRowClick?: (row: T) => void
  onSelectionChange?: (selectedRows: T[]) => void
  selectable?: boolean
  selectableKey?: keyof T
  refreshable?: boolean
  onRefresh?: () => void
  title?: string
  subtitle?: string
}

type SortDirection = 'asc' | 'desc' | null

interface SortConfig {
  column: string
  direction: SortDirection
}

export function MetricTable<T extends Record<string, any>>({
  data,
  columns,
  sortable = true,
  filterable = true,
  exportable = true,
  loading = false,
  emptyMessage = 'No data available',
  className = '',
  pageSize = 50,
  maxHeight = '600px',
  stickyHeader = true,
  striped = true,
  bordered = true,
  onRowClick,
  onSelectionChange,
  selectable = false,
  selectableKey = 'id' as keyof T,
  refreshable = false,
  onRefresh,
  title,
  subtitle,
}: MetricTableProps<T>) {
  const [sortConfig, setSortConfig] = useState<SortConfig>({ column: '', direction: null })
  const [filters, setFilters] = useState<Record<string, string>>({})
  const [globalFilter, setGlobalFilter] = useState('')
  const [currentPage, setCurrentPage] = useState(1)
  const [selectedRows, setSelectedRows] = useState<Set<any>>(new Set())
  const tableRef = useRef<HTMLDivElement>(null)

  // Handle column value extraction
  const getCellValue = (row: T, column: ColumnDef<T>) => {
    if (typeof column.accessor === 'function') {
      return column.accessor(row)
    }
    return row[column.accessor]
  }

  // Apply filters
  const filteredData = useMemo(() => {
    let result = [...data]

    // Apply global filter
    if (globalFilter) {
      result = result.filter(row =>
        columns.some(column => {
          const value = getCellValue(row, column)
          return String(value).toLowerCase().includes(globalFilter.toLowerCase())
        })
      )
    }

    // Apply column filters
    Object.entries(filters).forEach(([columnId, filterValue]) => {
      if (filterValue) {
        result = result.filter(row => {
          const column = columns.find(col => col.id === columnId)
          if (!column) return true
          
          const value = getCellValue(row, column)
          return String(value).toLowerCase().includes(filterValue.toLowerCase())
        })
      }
    })

    return result
  }, [data, columns, globalFilter, filters])

  // Apply sorting
  const sortedData = useMemo(() => {
    if (!sortConfig.column || !sortConfig.direction) {
      return filteredData
    }

    const column = columns.find(col => col.id === sortConfig.column)
    if (!column) return filteredData

    return [...filteredData].sort((a, b) => {
      if (column.sortFn) {
        const result = column.sortFn(a, b)
        return sortConfig.direction === 'asc' ? result : -result
      }

      const aValue = getCellValue(a, column)
      const bValue = getCellValue(b, column)

      if (aValue < bValue) return sortConfig.direction === 'asc' ? -1 : 1
      if (aValue > bValue) return sortConfig.direction === 'asc' ? 1 : -1
      return 0
    })
  }, [filteredData, sortConfig, columns])

  // Pagination
  const totalPages = Math.ceil(sortedData.length / pageSize)
  const paginatedData = sortedData.slice(
    (currentPage - 1) * pageSize,
    currentPage * pageSize
  )

  // Handle sorting
  const handleSort = (columnId: string) => {
    if (!sortable) return
    
    const column = columns.find(col => col.id === columnId)
    if (!column || column.sortable === false) return

    setSortConfig(prevConfig => {
      if (prevConfig.column === columnId) {
        const direction = prevConfig.direction === 'asc' ? 'desc' : prevConfig.direction === 'desc' ? null : 'asc'
        return { column: columnId, direction }
      }
      return { column: columnId, direction: 'asc' }
    })
  }

  // Handle row selection
  const handleRowSelect = (row: T) => {
    if (!selectable) return

    const key = row[selectableKey]
    const newSelectedRows = new Set(selectedRows)
    
    if (newSelectedRows.has(key)) {
      newSelectedRows.delete(key)
    } else {
      newSelectedRows.add(key)
    }
    
    setSelectedRows(newSelectedRows)
    onSelectionChange?.(data.filter(row => newSelectedRows.has(row[selectableKey])))
  }

  // Handle select all
  const handleSelectAll = () => {
    if (selectedRows.size === paginatedData.length) {
      setSelectedRows(new Set())
      onSelectionChange?.([])
    } else {
      const newSelectedRows = new Set(paginatedData.map(row => row[selectableKey]))
      setSelectedRows(newSelectedRows)
      onSelectionChange?.(paginatedData)
    }
  }

  // Clear filters
  const clearFilters = () => {
    setFilters({})
    setGlobalFilter('')
    setCurrentPage(1)
  }

  // Reset to first page when filters change
  useEffect(() => {
    setCurrentPage(1)
  }, [filters, globalFilter])

  const getSortIcon = (columnId: string) => {
    if (sortConfig.column !== columnId) return null
    return sortConfig.direction === 'asc' ? ChevronUpIcon : ChevronDownIcon
  }

  if (loading) {
    return (
      <div className={`card ${className}`}>
        <div className="flex items-center justify-center h-64">
          <LoadingSpinner size="lg" />
        </div>
      </div>
    )
  }

  return (
    <div className={`card ${className}`}>
      {/* Header */}
      {(title || subtitle || filterable || exportable || refreshable) && (
        <div className="card-header">
          <div className="flex items-center justify-between">
            <div>
              {title && (
                <h3 className="text-lg font-semibold text-gray-900">{title}</h3>
              )}
              {subtitle && (
                <p className="text-sm text-gray-600 mt-1">{subtitle}</p>
              )}
            </div>
            <div className="flex items-center space-x-3">
              {refreshable && (
                <button
                  onClick={onRefresh}
                  className="btn btn-outline"
                  disabled={loading}
                >
                  <ArrowPathIcon className="h-4 w-4 mr-2" />
                  Refresh
                </button>
              )}
              {exportable && (
                <ExportButton
                  data={filteredData}
                  filename={title ? `${title.replace(/\s+/g, '_').toLowerCase()}_export` : 'table_export'}
                  formats={['csv', 'json', 'xlsx']}
                />
              )}
            </div>
          </div>
        </div>
      )}

      {/* Filters */}
      {filterable && (
        <div className="p-4 border-b border-gray-200 bg-gray-50">
          <div className="flex items-center space-x-4">
            {/* Global search */}
            <div className="flex-1 relative">
              <MagnifyingGlassIcon className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-gray-400" />
              <input
                type="text"
                placeholder="Search all columns..."
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="input pl-10 w-full"
              />
            </div>

            {/* Filter indicator */}
            {(Object.keys(filters).length > 0 || globalFilter) && (
              <div className="flex items-center space-x-2">
                <span className="text-sm text-gray-500">
                  {Object.keys(filters).length + (globalFilter ? 1 : 0)} filter(s) active
                </span>
                <button
                  onClick={clearFilters}
                  className="text-primary-600 hover:text-primary-700 text-sm font-medium"
                >
                  Clear all
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Table */}
      <div
        ref={tableRef}
        className="overflow-auto"
        style={{ maxHeight }}
      >
        <table className="table">
          <thead className={`table-header ${stickyHeader ? 'sticky top-0 z-10' : ''}`}>
            <tr>
              {selectable && (
                <th className="table-header-cell w-12">
                  <input
                    type="checkbox"
                    checked={selectedRows.size === paginatedData.length && paginatedData.length > 0}
                    onChange={handleSelectAll}
                    className="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  />
                </th>
              )}
              {columns.map((column) => (
                <th
                  key={column.id}
                  className={`table-header-cell ${column.headerClassName || ''} ${
                    sortable && column.sortable !== false ? 'cursor-pointer hover:bg-gray-100' : ''
                  }`}
                  style={{
                    width: column.width,
                    minWidth: column.minWidth,
                    maxWidth: column.maxWidth,
                  }}
                  onClick={() => handleSort(column.id)}
                >
                  <div className="flex items-center justify-between">
                    <span>{column.header}</span>
                    {sortable && column.sortable !== false && (
                      <div className="flex items-center ml-2">
                        {getSortIcon(column.id) && (
                          <span className="inline-flex">
                            {React.createElement(getSortIcon(column.id)!, {
                              className: 'h-4 w-4 text-gray-400'
                            })}
                          </span>
                        )}
                        {filterable && column.filterable !== false && (
                          <FunnelIcon className="h-3 w-3 text-gray-400 ml-1" />
                        )}
                      </div>
                    )}
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {paginatedData.length === 0 ? (
              <tr>
                <td
                  colSpan={columns.length + (selectable ? 1 : 0)}
                  className="table-cell text-center text-gray-500 py-12"
                >
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              paginatedData.map((row, index) => (
                <tr
                  key={row[selectableKey] || index}
                  className={`
                    table-row
                    ${striped && index % 2 === 0 ? 'bg-gray-50' : ''}
                    ${bordered ? 'border-b' : ''}
                    ${onRowClick ? 'cursor-pointer' : ''}
                    ${selectedRows.has(row[selectableKey]) ? 'bg-primary-50' : ''}
                  `}
                  onClick={() => onRowClick?.(row)}
                >
                  {selectable && (
                    <td className="table-cell w-12">
                      <input
                        type="checkbox"
                        checked={selectedRows.has(row[selectableKey])}
                        onChange={() => handleRowSelect(row)}
                        onClick={(e) => e.stopPropagation()}
                        className="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      />
                    </td>
                  )}
                  {columns.map((column) => {
                    const value = getCellValue(row, column)
                    return (
                      <td
                        key={column.id}
                        className={`table-cell ${column.className || ''}`}
                        style={{
                          width: column.width,
                          minWidth: column.minWidth,
                          maxWidth: column.maxWidth,
                        }}
                      >
                        {column.render ? column.render(value, row) : String(value)}
                      </td>
                    )
                  })}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="card-footer">
          <div className="flex items-center justify-between">
            <div className="text-sm text-gray-600">
              Showing {((currentPage - 1) * pageSize) + 1} to {Math.min(currentPage * pageSize, sortedData.length)} of {sortedData.length} results
            </div>
            <div className="flex items-center space-x-2">
              <button
                onClick={() => setCurrentPage(prev => Math.max(1, prev - 1))}
                disabled={currentPage === 1}
                className="btn btn-outline disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Previous
              </button>
              <span className="text-sm text-gray-600">
                Page {currentPage} of {totalPages}
              </span>
              <button
                onClick={() => setCurrentPage(prev => Math.min(totalPages, prev + 1))}
                disabled={currentPage === totalPages}
                className="btn btn-outline disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Next
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default MetricTable
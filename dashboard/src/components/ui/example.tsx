/**
 * Example usage of the UI components
 * This file demonstrates how to use the new reusable UI components
 */

import React from 'react'
import { ExpandableSection, MetricTable, ExportButton } from './index'
import type { ColumnDef } from './MetricTable'

// Example data structure
interface ExampleMetric {
  id: string
  name: string
  value: number
  timestamp: string
  status: 'success' | 'warning' | 'error'
  latency: number
}

// Example usage component
export function UIComponentsExample() {
  // Sample data
  const sampleData: ExampleMetric[] = [
    {
      id: '1',
      name: 'API Response Time',
      value: 245.5,
      timestamp: '2024-01-01T10:00:00Z',
      status: 'success',
      latency: 245.5,
    },
    {
      id: '2',
      name: 'Database Query',
      value: 120.3,
      timestamp: '2024-01-01T10:01:00Z',
      status: 'warning',
      latency: 120.3,
    },
    {
      id: '3',
      name: 'Cache Hit Rate',
      value: 95.2,
      timestamp: '2024-01-01T10:02:00Z',
      status: 'success',
      latency: 45.1,
    },
  ]

  // Table columns configuration
  const columns: ColumnDef<ExampleMetric>[] = [
    {
      id: 'name',
      header: 'Metric Name',
      accessor: 'name',
      sortable: true,
      filterable: true,
    },
    {
      id: 'value',
      header: 'Value',
      accessor: 'value',
      sortable: true,
      render: (value: number) => `${value.toFixed(2)}ms`,
    },
    {
      id: 'status',
      header: 'Status',
      accessor: 'status',
      sortable: true,
      filterable: true,
      render: (status: string) => (
        <span className={`badge ${
          status === 'success' ? 'badge-success' : 
          status === 'warning' ? 'badge-warning' : 
          'badge-danger'
        }`}>
          {status}
        </span>
      ),
    },
    {
      id: 'timestamp',
      header: 'Timestamp',
      accessor: 'timestamp',
      sortable: true,
      render: (timestamp: string) => new Date(timestamp).toLocaleString(),
    },
  ]

  return (
    <div className="p-6 space-y-6 bg-gray-50 min-h-screen">
      <h1 className="text-3xl font-bold text-gray-900">UI Components Demo</h1>
      
      {/* ExpandableSection Examples */}
      <ExpandableSection
        title="Basic Expandable Section"
        subtitle="Click to expand/collapse"
        defaultExpanded={true}
      >
        <p className="text-gray-600">
          This is a basic expandable section with smooth animations.
          You can put any content here.
        </p>
      </ExpandableSection>

      <ExpandableSection
        title="Loading State Example"
        subtitle="Showing loading spinner"
        isLoading={true}
      >
        <p>This content is behind a loading state.</p>
      </ExpandableSection>

      <ExpandableSection
        title="Error State Example"
        subtitle="Showing error message"
        error="Something went wrong!"
      >
        <p>This content has an error.</p>
      </ExpandableSection>

      <ExpandableSection
        title="Success Variant with Actions"
        subtitle="Success styling with header actions"
        variant="success"
        headerActions={
          <button className="btn btn-primary">
            Action
          </button>
        }
      >
        <p className="text-success-700">
          This is a success variant with header actions.
        </p>
      </ExpandableSection>

      {/* MetricTable Example */}
      <ExpandableSection
        title="Metric Table Example"
        subtitle="Sortable, filterable, and exportable table"
        defaultExpanded={true}
      >
        <MetricTable
          data={sampleData}
          columns={columns}
          title="Performance Metrics"
          subtitle="Real-time performance data"
          sortable={true}
          filterable={true}
          exportable={true}
          selectable={true}
          refreshable={true}
          onRefresh={() => console.log('Refreshing data...')}
          onRowClick={(row) => console.log('Row clicked:', row)}
          onSelectionChange={(selected) => console.log('Selection changed:', selected)}
        />
      </ExpandableSection>

      {/* ExportButton Examples */}
      <ExpandableSection
        title="Export Button Examples"
        subtitle="Different export button configurations"
        defaultExpanded={true}
      >
        <div className="space-y-4">
          <div className="flex items-center space-x-4">
            <ExportButton
              data={sampleData}
              filename="single_format_export"
              formats={['json']}
              variant="primary"
            />
            
            <ExportButton
              data={sampleData}
              filename="multi_format_export"
              formats={['json', 'csv', 'xlsx']}
              variant="outline"
            />
            
            <ExportButton
              data={sampleData}
              filename="custom_export"
              formats={['json', 'csv']}
              variant="secondary"
              size="lg"
              onExport={(format, data) => {
                console.log(`Custom export for ${format}:`, data)
                // Custom export logic here
              }}
            />
          </div>
        </div>
      </ExpandableSection>

      {/* Complex Example */}
      <ExpandableSection
        title="Complex Integration Example"
        subtitle="Multiple components working together"
        defaultExpanded={false}
        headerActions={
          <div className="flex items-center space-x-2">
            <ExportButton
              data={sampleData}
              filename="complex_metrics"
              formats={['json', 'csv']}
              size="sm"
            />
            <button className="btn btn-secondary">
              Settings
            </button>
          </div>
        }
      >
        <div className="space-y-4">
          <MetricTable
            data={sampleData}
            columns={columns.slice(0, 3)} // Show fewer columns
            sortable={true}
            filterable={true}
            exportable={false} // Disable built-in export since we have one in header
            pageSize={5}
            maxHeight="300px"
            striped={true}
            bordered={true}
          />
          
          <div className="bg-gray-50 p-4 rounded-lg">
            <h4 className="font-semibold text-gray-900 mb-2">Additional Information</h4>
            <p className="text-sm text-gray-600">
              This example shows how the components can be combined to create
              complex interfaces with consistent styling and behavior.
            </p>
          </div>
        </div>
      </ExpandableSection>
    </div>
  )
}

export default UIComponentsExample
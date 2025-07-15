import React, { useState, useMemo } from 'react'
import { DetailedMetrics, SystemMetrics } from '../../types/detailed-metrics'
import { MetricCard } from '../index'
import { formatBytes, formatPercentage } from '../../utils/metric-formatters'
import {
  CpuChipIcon,
  CircleStackIcon,
  ServerIcon,
  SignalIcon,
  ExclamationTriangleIcon,
  CheckCircleIcon,
  InformationCircleIcon,
  ArrowTrendingUpIcon,
  ArrowTrendingDownIcon,
  ChartBarIcon,
  DocumentArrowDownIcon,
  LightBulbIcon,
  ComputerDesktopIcon,
  ClockIcon,
  BoltIcon,
  BeakerIcon,
  CloudIcon,
  CogIcon,
  BuildingLibraryIcon,
  ScaleIcon,
  WrenchScrewdriverIcon,
  ShieldCheckIcon,
  FireIcon,
  BanknotesIcon,
  StarIcon,
  SparklesIcon,
  MagnifyingGlassIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  EyeIcon,
  EyeSlashIcon,
  PresentationChartLineIcon,
  TableCellsIcon,
  DocumentTextIcon,
  ShareIcon,
  PrinterIcon,
  ClipboardDocumentIcon,
  DocumentChartBarIcon,
  CameraIcon,
  PhotoIcon
} from '@heroicons/react/24/outline'
import { ExpandableSection, ExportButton } from '../ui'

interface SystemMetricsPanelProps {
  data: DetailedMetrics
  showEnvironment?: boolean
  showResourceUsage?: boolean
  compact?: boolean
}

interface ResourceThreshold {
  warning: number
  critical: number
  optimal: number
}

interface SystemRecommendation {
  category: string
  priority: 'low' | 'medium' | 'high' | 'critical'
  title: string
  description: string
  impact: string
  action: string
  estimatedImprovement: number
  resources: string[]
}

const RESOURCE_THRESHOLDS: Record<string, ResourceThreshold> = {
  cpu: { warning: 70, critical: 90, optimal: 50 },
  memory: { warning: 80, critical: 95, optimal: 60 },
  disk: { warning: 85, critical: 95, optimal: 70 },
  network: { warning: 80, critical: 90, optimal: 60 }
}

const formatCPUUsage = (percentage: number): string => {
  return `${percentage.toFixed(1)}%`
}

const formatNetworkSpeed = (bytesPerSecond: number): string => {
  if (bytesPerSecond === 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let value = bytesPerSecond
  let unitIndex = 0
  
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  
  return `${value.toFixed(1)} ${units[unitIndex]}`
}

const formatIOPS = (iops: number): string => {
  if (iops === 0) return '0 IOPS'
  if (iops < 1000) return `${Math.round(iops)} IOPS`
  if (iops < 1000000) return `${(iops / 1000).toFixed(1)}K IOPS`
  return `${(iops / 1000000).toFixed(1)}M IOPS`
}

const formatUptime = (seconds: number): string => {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`
  return `${Math.round(seconds / 86400)}d`
}

const getResourceHealthColor = (value: number, thresholds: ResourceThreshold): string => {
  if (value >= thresholds.critical) return 'text-red-600'
  if (value >= thresholds.warning) return 'text-orange-600'
  if (value <= thresholds.optimal) return 'text-green-600'
  return 'text-yellow-600'
}

const getResourceHealthVariant = (value: number, thresholds: ResourceThreshold): 'success' | 'warning' | 'danger' | 'default' => {
  if (value >= thresholds.critical) return 'danger'
  if (value >= thresholds.warning) return 'warning'
  if (value <= thresholds.optimal) return 'success'
  return 'default'
}

const generateSystemRecommendations = (systemMetrics: SystemMetrics): SystemRecommendation[] => {
  const recommendations: SystemRecommendation[] = []
  
  // CPU Analysis
  if (systemMetrics.cpuUsage > RESOURCE_THRESHOLDS.cpu.critical) {
    recommendations.push({
      category: 'Performance',
      priority: 'critical',
      title: 'Critical CPU Usage',
      description: 'CPU usage is critically high and may impact system performance',
      impact: 'Severe performance degradation and potential system instability',
      action: 'Reduce workload, optimize processes, or scale resources',
      estimatedImprovement: 40,
      resources: ['CPU', 'Performance']
    })
  } else if (systemMetrics.cpuUsage > RESOURCE_THRESHOLDS.cpu.warning) {
    recommendations.push({
      category: 'Performance',
      priority: 'high',
      title: 'High CPU Usage',
      description: 'CPU usage is approaching critical levels',
      impact: 'Potential performance issues and increased response times',
      action: 'Monitor workload and consider resource optimization',
      estimatedImprovement: 25,
      resources: ['CPU']
    })
  }
  
  // Memory Analysis
  if (systemMetrics.memoryUsage > RESOURCE_THRESHOLDS.memory.critical) {
    recommendations.push({
      category: 'Memory',
      priority: 'critical',
      title: 'Critical Memory Usage',
      description: 'Memory usage is critically high, risk of system instability',
      impact: 'Potential memory allocation failures and system crashes',
      action: 'Free memory, optimize memory usage, or add more RAM',
      estimatedImprovement: 50,
      resources: ['Memory', 'Stability']
    })
  } else if (systemMetrics.memoryUsage > RESOURCE_THRESHOLDS.memory.warning) {
    recommendations.push({
      category: 'Memory',
      priority: 'high',
      title: 'High Memory Usage',
      description: 'Memory usage is approaching critical levels',
      impact: 'Increased garbage collection and slower performance',
      action: 'Monitor memory usage and optimize application memory consumption',
      estimatedImprovement: 30,
      resources: ['Memory']
    })
  }
  
  // Disk Analysis
  if (systemMetrics.diskIO.utilization > RESOURCE_THRESHOLDS.disk.critical) {
    recommendations.push({
      category: 'Storage',
      priority: 'high',
      title: 'High Disk Utilization',
      description: 'Disk utilization is critically high',
      impact: 'Slow I/O operations and potential system bottlenecks',
      action: 'Optimize disk usage, add faster storage, or implement caching',
      estimatedImprovement: 35,
      resources: ['Storage', 'I/O']
    })
  }
  
  // Network Analysis
  if (systemMetrics.networkIO.latency > 100) {
    recommendations.push({
      category: 'Network',
      priority: 'medium',
      title: 'High Network Latency',
      description: 'Network latency is affecting performance',
      impact: 'Slower response times for network operations',
      action: 'Optimize network configuration or consider CDN implementation',
      estimatedImprovement: 20,
      resources: ['Network']
    })
  }
  
  // General Optimizations
  if (systemMetrics.healthScore < 70) {
    recommendations.push({
      category: 'General',
      priority: 'medium',
      title: 'System Health Optimization',
      description: 'Overall system health could be improved',
      impact: 'Better overall performance and reliability',
      action: 'Implement comprehensive monitoring and optimization strategies',
      estimatedImprovement: 15,
      resources: ['General', 'Monitoring']
    })
  }
  
  // Capacity Planning
  if (systemMetrics.capacity.current / systemMetrics.capacity.maximum > 0.8) {
    recommendations.push({
      category: 'Capacity',
      priority: 'high',
      title: 'Capacity Planning Required',
      description: 'System is approaching capacity limits',
      impact: 'Potential service degradation as load increases',
      action: 'Plan for capacity scaling or resource allocation',
      estimatedImprovement: 25,
      resources: ['Capacity', 'Scaling']
    })
  }
  
  return recommendations.sort((a, b) => {
    const priorityOrder = { critical: 4, high: 3, medium: 2, low: 1 }
    return priorityOrder[b.priority] - priorityOrder[a.priority]
  })
}

const exportSystemReport = (data: DetailedMetrics, format: 'json' | 'csv' | 'pdf' = 'json') => {
  const systemData = {
    timestamp: data.timestamp,
    runId: data.runId,
    systemMetrics: data.systemMetrics,
    environment: data.environment,
    summary: {
      healthScore: data.systemMetrics.healthScore,
      cpuUsage: data.systemMetrics.cpuUsage,
      memoryUsage: data.systemMetrics.memoryUsage,
      diskUtilization: data.systemMetrics.diskIO.utilization,
      networkLatency: data.systemMetrics.networkIO.latency,
      bottlenecks: data.systemMetrics.bottlenecks.length,
      recommendations: generateSystemRecommendations(data.systemMetrics).length
    }
  }
  
  const filename = `system-metrics-${data.runId}-${new Date().toISOString().split('T')[0]}`
  
  switch (format) {
    case 'json':
      const jsonBlob = new Blob([JSON.stringify(systemData, null, 2)], { type: 'application/json' })
      const jsonUrl = URL.createObjectURL(jsonBlob)
      const jsonLink = document.createElement('a')
      jsonLink.href = jsonUrl
      jsonLink.download = `${filename}.json`
      document.body.appendChild(jsonLink)
      jsonLink.click()
      document.body.removeChild(jsonLink)
      URL.revokeObjectURL(jsonUrl)
      break
      
    case 'csv':
      const csvData = [
        ['Metric', 'Value', 'Unit', 'Status'],
        ['CPU Usage', data.systemMetrics.cpuUsage.toFixed(1), '%', data.systemMetrics.cpuUsage > 80 ? 'Warning' : 'OK'],
        ['Memory Usage', data.systemMetrics.memoryUsage.toFixed(1), '%', data.systemMetrics.memoryUsage > 80 ? 'Warning' : 'OK'],
        ['Disk Utilization', data.systemMetrics.diskIO.utilization.toFixed(1), '%', data.systemMetrics.diskIO.utilization > 80 ? 'Warning' : 'OK'],
        ['Network Latency', data.systemMetrics.networkIO.latency.toFixed(1), 'ms', data.systemMetrics.networkIO.latency > 100 ? 'Warning' : 'OK'],
        ['Health Score', data.systemMetrics.healthScore.toFixed(1), '/100', data.systemMetrics.healthScore < 70 ? 'Warning' : 'OK'],
        ['Bottlenecks', data.systemMetrics.bottlenecks.length.toString(), 'count', data.systemMetrics.bottlenecks.length > 0 ? 'Warning' : 'OK']
      ]
      
      const csvContent = csvData.map(row => row.join(',')).join('\n')
      const csvBlob = new Blob([csvContent], { type: 'text/csv' })
      const csvUrl = URL.createObjectURL(csvBlob)
      const csvLink = document.createElement('a')
      csvLink.href = csvUrl
      csvLink.download = `${filename}.csv`
      document.body.appendChild(csvLink)
      csvLink.click()
      document.body.removeChild(csvLink)
      URL.revokeObjectURL(csvUrl)
      break
      
    case 'pdf':
      // For PDF, we'll create a detailed text report
      const pdfContent = `
SYSTEM METRICS REPORT
Generated: ${new Date().toISOString()}
Run ID: ${data.runId}

SYSTEM OVERVIEW
Health Score: ${data.systemMetrics.healthScore}/100
CPU Usage: ${data.systemMetrics.cpuUsage.toFixed(1)}%
Memory Usage: ${data.systemMetrics.memoryUsage.toFixed(1)}%
Disk Utilization: ${data.systemMetrics.diskIO.utilization.toFixed(1)}%
Network Latency: ${data.systemMetrics.networkIO.latency.toFixed(1)}ms

ENVIRONMENT INFORMATION
OS: ${data.environment.os}
Architecture: ${data.environment.architecture}
CPU: ${data.environment.cpuModel} (${data.environment.cpuCores} cores)
Memory: ${data.environment.totalMemoryGB} GB
Network: ${data.environment.networkType}
Region: ${data.environment.region}

RESOURCE UTILIZATION
CPU: ${data.systemMetrics.resourceUtilization.cpu.current.toFixed(1)}% (avg: ${data.systemMetrics.resourceUtilization.cpu.average.toFixed(1)}%, peak: ${data.systemMetrics.resourceUtilization.cpu.peak.toFixed(1)}%)
Memory: ${data.systemMetrics.resourceUtilization.memory.current.toFixed(1)}% (avg: ${data.systemMetrics.resourceUtilization.memory.average.toFixed(1)}%, peak: ${data.systemMetrics.resourceUtilization.memory.peak.toFixed(1)}%)
Network: ${data.systemMetrics.resourceUtilization.network.current.toFixed(1)}% (avg: ${data.systemMetrics.resourceUtilization.network.average.toFixed(1)}%, peak: ${data.systemMetrics.resourceUtilization.network.peak.toFixed(1)}%)
Disk: ${data.systemMetrics.resourceUtilization.disk.current.toFixed(1)}% (avg: ${data.systemMetrics.resourceUtilization.disk.average.toFixed(1)}%, peak: ${data.systemMetrics.resourceUtilization.disk.peak.toFixed(1)}%)

BOTTLENECKS
${data.systemMetrics.bottlenecks.map(b => `- ${b.type.toUpperCase()}: ${b.description} (${b.severity})`).join('\n')}

RECOMMENDATIONS
${generateSystemRecommendations(data.systemMetrics).map(r => `- ${r.title}: ${r.description}`).join('\n')}
      `
      
      const pdfBlob = new Blob([pdfContent], { type: 'text/plain' })
      const pdfUrl = URL.createObjectURL(pdfBlob)
      const pdfLink = document.createElement('a')
      pdfLink.href = pdfUrl
      pdfLink.download = `${filename}.txt`
      document.body.appendChild(pdfLink)
      pdfLink.click()
      document.body.removeChild(pdfLink)
      URL.revokeObjectURL(pdfUrl)
      break
  }
}

export function SystemMetricsPanel({
  data,
  showEnvironment = true,
  showResourceUsage = true,
  compact = false
}: SystemMetricsPanelProps) {
  const [showDetails, setShowDetails] = useState(!compact)
  const [selectedView, setSelectedView] = useState<'overview' | 'detailed' | 'trends'>('overview')
  const [showRecommendations, setShowRecommendations] = useState(false)
  
  const systemMetrics = data.systemMetrics
  const recommendations = useMemo(() => generateSystemRecommendations(systemMetrics), [systemMetrics])
  
  const cpuThreshold = RESOURCE_THRESHOLDS.cpu
  const memoryThreshold = RESOURCE_THRESHOLDS.memory
  const diskThreshold = RESOURCE_THRESHOLDS.disk
  const networkThreshold = RESOURCE_THRESHOLDS.network
  
  const healthScoreColor = systemMetrics.healthScore >= 80 ? 'text-green-600' : 
                          systemMetrics.healthScore >= 60 ? 'text-yellow-600' : 'text-red-600'
  
  const healthVariant = systemMetrics.healthScore >= 80 ? 'success' : 
                       systemMetrics.healthScore >= 60 ? 'warning' : 'danger'
  
  const criticalIssues = recommendations.filter(r => r.priority === 'critical').length
  const highIssues = recommendations.filter(r => r.priority === 'high').length
  const totalBottlenecks = systemMetrics.bottlenecks.length
  
  const capacityUtilization = (systemMetrics.capacity.current / systemMetrics.capacity.maximum) * 100
  const timeToCapacity = systemMetrics.capacity.timeToCapacity
  
  if (compact) {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-lg font-semibold flex items-center">
            <ServerIcon className="h-5 w-5 mr-2" />
            System Overview
          </h3>
          <div className="flex items-center space-x-2">
            <button
              onClick={() => setShowDetails(!showDetails)}
              className="flex items-center text-sm text-gray-600 hover:text-gray-900"
            >
              {showDetails ? (
                <>
                  <EyeSlashIcon className="h-4 w-4 mr-1" />
                  Hide Details
                </>
              ) : (
                <>
                  <EyeIcon className="h-4 w-4 mr-1" />
                  Show Details
                </>
              )}
            </button>
            <ExportButton
              onExport={(format) => exportSystemReport(data, format as 'json' | 'csv' | 'pdf')}
              formats={['json', 'csv', 'pdf']}
              filename={`system-metrics-${data.runId}`}
              size="sm"
            />
          </div>
        </div>
        
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <MetricCard
            title="Health Score"
            value={systemMetrics.healthScore}
            format="number"
            precision={0}
            unit="/100"
            size="sm"
            variant={healthVariant}
            icon={ShieldCheckIcon}
          />
          
          <MetricCard
            title="CPU Usage"
            value={systemMetrics.cpuUsage}
            format="number"
            precision={1}
            unit="%"
            size="sm"
            variant={getResourceHealthVariant(systemMetrics.cpuUsage, cpuThreshold)}
            icon={CpuChipIcon}
          />
          
          <MetricCard
            title="Memory Usage"
            value={systemMetrics.memoryUsage}
            format="number"
            precision={1}
            unit="%"
            size="sm"
            variant={getResourceHealthVariant(systemMetrics.memoryUsage, memoryThreshold)}
            icon={CircleStackIcon}
          />
          
          <MetricCard
            title="Issues"
            value={criticalIssues + highIssues}
            format="number"
            precision={0}
            size="sm"
            variant={criticalIssues > 0 ? 'danger' : highIssues > 0 ? 'warning' : 'success'}
            icon={ExclamationTriangleIcon}
          />
        </div>
        
        {showDetails && (
          <div className="mt-4 space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="bg-gray-50 p-4 rounded-lg">
                <h4 className="font-medium mb-2">Network I/O</h4>
                <div className="space-y-1 text-sm">
                  <div>Latency: {systemMetrics.networkIO.latency.toFixed(1)}ms</div>
                  <div>Throughput: {formatNetworkSpeed(systemMetrics.networkIO.throughput)}</div>
                  <div>Packet Loss: {formatPercentage(systemMetrics.networkIO.packetLoss)}</div>
                </div>
              </div>
              
              <div className="bg-gray-50 p-4 rounded-lg">
                <h4 className="font-medium mb-2">Disk I/O</h4>
                <div className="space-y-1 text-sm">
                  <div>Utilization: {formatPercentage(systemMetrics.diskIO.utilization)}</div>
                  <div>IOPS: {formatIOPS(systemMetrics.diskIO.iops)}</div>
                  <div>Queue Depth: {systemMetrics.diskIO.queueDepth}</div>
                </div>
              </div>
            </div>
            
            {totalBottlenecks > 0 && (
              <div className="bg-red-50 border border-red-200 p-4 rounded-lg">
                <h4 className="font-medium text-red-800 mb-2">Active Bottlenecks</h4>
                <div className="space-y-1">
                  {systemMetrics.bottlenecks.slice(0, 3).map((bottleneck, index) => (
                    <div key={index} className="text-sm text-red-700">
                      {bottleneck.type.toUpperCase()}: {bottleneck.description}
                    </div>
                  ))}
                  {systemMetrics.bottlenecks.length > 3 && (
                    <div className="text-sm text-red-600">
                      +{systemMetrics.bottlenecks.length - 3} more bottlenecks
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    )
  }
  
  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-gray-900 flex items-center">
            <ServerIcon className="h-7 w-7 mr-3" />
            System Metrics
          </h2>
          <p className="text-sm text-gray-600 mt-1">
            Real-time system resource monitoring and analysis
          </p>
        </div>
        
        <div className="flex items-center space-x-3 mt-4 sm:mt-0">
          <div className="flex bg-gray-100 rounded-lg p-1">
            <button
              onClick={() => setSelectedView('overview')}
              className={`px-3 py-1 text-sm rounded-md ${
                selectedView === 'overview' ? 'bg-white shadow-sm' : 'text-gray-600'
              }`}
            >
              Overview
            </button>
            <button
              onClick={() => setSelectedView('detailed')}
              className={`px-3 py-1 text-sm rounded-md ${
                selectedView === 'detailed' ? 'bg-white shadow-sm' : 'text-gray-600'
              }`}
            >
              Detailed
            </button>
            <button
              onClick={() => setSelectedView('trends')}
              className={`px-3 py-1 text-sm rounded-md ${
                selectedView === 'trends' ? 'bg-white shadow-sm' : 'text-gray-600'
              }`}
            >
              Trends
            </button>
          </div>
          
          <ExportButton
            onExport={(format) => exportSystemReport(data, format as 'json' | 'csv' | 'pdf')}
            formats={['json', 'csv', 'pdf']}
            filename={`system-metrics-${data.runId}`}
          />
        </div>
      </div>
      
      {/* System Health Overview */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="System Health Score"
          value={systemMetrics.healthScore}
          format="number"
          precision={0}
          unit="/100"
          variant={healthVariant}
          icon={ShieldCheckIcon}
          subtitle="Overall system health"
        />
        
        <MetricCard
          title="Active Bottlenecks"
          value={totalBottlenecks}
          format="number"
          precision={0}
          variant={totalBottlenecks > 0 ? 'danger' : 'success'}
          icon={totalBottlenecks > 0 ? ExclamationTriangleIcon : CheckCircleIcon}
          subtitle="Performance bottlenecks"
        />
        
        <MetricCard
          title="Capacity Utilization"
          value={capacityUtilization}
          format="number"
          precision={1}
          unit="%"
          variant={capacityUtilization > 80 ? 'danger' : capacityUtilization > 60 ? 'warning' : 'success'}
          icon={ScaleIcon}
          subtitle="Current capacity usage"
        />
        
        <MetricCard
          title="Time to Capacity"
          value={timeToCapacity}
          format="number"
          precision={0}
          unit="days"
          variant={timeToCapacity < 30 ? 'danger' : timeToCapacity < 90 ? 'warning' : 'success'}
          icon={ClockIcon}
          subtitle="Estimated time to full capacity"
        />
      </div>
      
      {showResourceUsage && (
        <ExpandableSection title="Resource Usage" defaultExpanded={true}>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <MetricCard
              title="CPU Usage"
              value={systemMetrics.cpuUsage}
              format="number"
              precision={1}
              unit="%"
              variant={getResourceHealthVariant(systemMetrics.cpuUsage, cpuThreshold)}
              icon={CpuChipIcon}
              subtitle={`Peak: ${systemMetrics.resourceUtilization.cpu.peak.toFixed(1)}%`}
            />
            
            <MetricCard
              title="Memory Usage"
              value={systemMetrics.memoryUsage}
              format="number"
              precision={1}
              unit="%"
              variant={getResourceHealthVariant(systemMetrics.memoryUsage, memoryThreshold)}
              icon={CircleStackIcon}
              subtitle={`Peak: ${systemMetrics.resourceUtilization.memory.peak.toFixed(1)}%`}
            />
            
            <MetricCard
              title="Network I/O"
              value={systemMetrics.networkIO.throughput}
              format="bytes"
              precision={1}
              unit="/s"
              variant={getResourceHealthVariant(systemMetrics.resourceUtilization.network.current, networkThreshold)}
              icon={SignalIcon}
              subtitle={`Latency: ${systemMetrics.networkIO.latency.toFixed(1)}ms`}
            />
            
            <MetricCard
              title="Disk I/O"
              value={systemMetrics.diskIO.utilization}
              format="number"
              precision={1}
              unit="%"
              variant={getResourceHealthVariant(systemMetrics.diskIO.utilization, diskThreshold)}
              icon={CircleStackIcon}
              subtitle={`IOPS: ${formatIOPS(systemMetrics.diskIO.iops)}`}
            />
          </div>
          
          {/* Detailed Resource Metrics */}
          {selectedView === 'detailed' && (
            <div className="mt-6 space-y-4">
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {/* CPU Details */}
                <div className="bg-white border border-gray-200 rounded-lg p-6">
                  <h4 className="font-semibold mb-4 flex items-center">
                    <CpuChipIcon className="h-5 w-5 mr-2" />
                    CPU Metrics
                  </h4>
                  <div className="space-y-3">
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Current Usage</span>
                      <span className={`font-medium ${getResourceHealthColor(systemMetrics.cpuUsage, cpuThreshold)}`}>
                        {formatCPUUsage(systemMetrics.cpuUsage)}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Average Usage</span>
                      <span className="font-medium">{formatCPUUsage(systemMetrics.resourceUtilization.cpu.average)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Peak Usage</span>
                      <span className="font-medium">{formatCPUUsage(systemMetrics.resourceUtilization.cpu.peak)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Efficiency</span>
                      <span className="font-medium">{formatPercentage(systemMetrics.resourceUtilization.cpu.efficiency)}</span>
                    </div>
                  </div>
                </div>
                
                {/* Memory Details */}
                <div className="bg-white border border-gray-200 rounded-lg p-6">
                  <h4 className="font-semibold mb-4 flex items-center">
                    <CircleStackIcon className="h-5 w-5 mr-2" />
                    Memory Metrics
                  </h4>
                  <div className="space-y-3">
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Current Usage</span>
                      <span className={`font-medium ${getResourceHealthColor(systemMetrics.memoryUsage, memoryThreshold)}`}>
                        {formatPercentage(systemMetrics.memoryUsage)}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Average Usage</span>
                      <span className="font-medium">{formatPercentage(systemMetrics.resourceUtilization.memory.average)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Peak Usage</span>
                      <span className="font-medium">{formatPercentage(systemMetrics.resourceUtilization.memory.peak)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Efficiency</span>
                      <span className="font-medium">{formatPercentage(systemMetrics.resourceUtilization.memory.efficiency)}</span>
                    </div>
                  </div>
                </div>
                
                {/* Network Details */}
                <div className="bg-white border border-gray-200 rounded-lg p-6">
                  <h4 className="font-semibold mb-4 flex items-center">
                    <SignalIcon className="h-5 w-5 mr-2" />
                    Network Metrics
                  </h4>
                  <div className="space-y-3">
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Latency</span>
                      <span className="font-medium">{systemMetrics.networkIO.latency.toFixed(1)}ms</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Throughput</span>
                      <span className="font-medium">{formatNetworkSpeed(systemMetrics.networkIO.throughput)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Packet Loss</span>
                      <span className="font-medium">{formatPercentage(systemMetrics.networkIO.packetLoss)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Jitter</span>
                      <span className="font-medium">{systemMetrics.networkIO.jitter.toFixed(1)}ms</span>
                    </div>
                  </div>
                </div>
                
                {/* Disk Details */}
                <div className="bg-white border border-gray-200 rounded-lg p-6">
                  <h4 className="font-semibold mb-4 flex items-center">
                    <CircleStackIcon className="h-5 w-5 mr-2" />
                    Disk Metrics
                  </h4>
                  <div className="space-y-3">
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Utilization</span>
                      <span className={`font-medium ${getResourceHealthColor(systemMetrics.diskIO.utilization, diskThreshold)}`}>
                        {formatPercentage(systemMetrics.diskIO.utilization)}
                      </span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">IOPS</span>
                      <span className="font-medium">{formatIOPS(systemMetrics.diskIO.iops)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Queue Depth</span>
                      <span className="font-medium">{systemMetrics.diskIO.queueDepth}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-gray-600">Free Space</span>
                      <span className="font-medium">{formatBytes(systemMetrics.diskIO.freeSpace)}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}
        </ExpandableSection>
      )}
      
      {/* Bottleneck Analysis */}
      {totalBottlenecks > 0 && (
        <ExpandableSection title="Bottleneck Analysis" defaultExpanded={true}>
          <div className="space-y-4">
            {systemMetrics.bottlenecks.map((bottleneck, index) => (
              <div key={index} className="bg-white border border-gray-200 rounded-lg p-6">
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center mb-2">
                      <div className={`w-2 h-2 rounded-full mr-3 ${
                        bottleneck.severity === 'critical' ? 'bg-red-500' :
                        bottleneck.severity === 'high' ? 'bg-orange-500' :
                        bottleneck.severity === 'medium' ? 'bg-yellow-500' : 'bg-green-500'
                      }`}></div>
                      <h4 className="font-semibold text-gray-900">{bottleneck.type.toUpperCase()} Bottleneck</h4>
                      <span className={`ml-2 px-2 py-1 text-xs rounded-full ${
                        bottleneck.severity === 'critical' ? 'bg-red-100 text-red-800' :
                        bottleneck.severity === 'high' ? 'bg-orange-100 text-orange-800' :
                        bottleneck.severity === 'medium' ? 'bg-yellow-100 text-yellow-800' : 'bg-green-100 text-green-800'
                      }`}>
                        {bottleneck.severity}
                      </span>
                    </div>
                    <p className="text-gray-600 mb-3">{bottleneck.description}</p>
                    <div className="text-sm text-gray-500">
                      Impact: {bottleneck.impact}% • Estimated Improvement: {bottleneck.estimatedImprovement}%
                    </div>
                  </div>
                  <div className="ml-4">
                    <LightBulbIcon className="h-6 w-6 text-yellow-500" />
                  </div>
                </div>
                <div className="mt-4 bg-gray-50 rounded-lg p-3">
                  <h5 className="font-medium text-gray-900 mb-2">Recommendations</h5>
                  <ul className="space-y-1">
                    {bottleneck.recommendations.map((rec, recIndex) => (
                      <li key={recIndex} className="text-sm text-gray-600">• {rec}</li>
                    ))}
                  </ul>
                </div>
              </div>
            ))}
          </div>
        </ExpandableSection>
      )}
      
      {/* Environment Information */}
      {showEnvironment && (
        <ExpandableSection title="Environment Information" defaultExpanded={false}>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            <div className="bg-white border border-gray-200 rounded-lg p-6">
              <h4 className="font-semibold mb-4 flex items-center">
                <ComputerDesktopIcon className="h-5 w-5 mr-2" />
                System Information
              </h4>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Operating System</span>
                  <span className="font-medium">{data.environment.os}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Architecture</span>
                  <span className="font-medium">{data.environment.architecture}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Containerized</span>
                  <span className="font-medium">{data.environment.containerized ? 'Yes' : 'No'}</span>
                </div>
                {data.environment.cloudProvider && (
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Cloud Provider</span>
                    <span className="font-medium">{data.environment.cloudProvider}</span>
                  </div>
                )}
              </div>
            </div>
            
            <div className="bg-white border border-gray-200 rounded-lg p-6">
              <h4 className="font-semibold mb-4 flex items-center">
                <CpuChipIcon className="h-5 w-5 mr-2" />
                Hardware Specifications
              </h4>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">CPU Model</span>
                  <span className="font-medium">{data.environment.cpuModel}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">CPU Cores</span>
                  <span className="font-medium">{data.environment.cpuCores}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Total Memory</span>
                  <span className="font-medium">{data.environment.totalMemoryGB} GB</span>
                </div>
                {data.environment.instanceType && (
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Instance Type</span>
                    <span className="font-medium">{data.environment.instanceType}</span>
                  </div>
                )}
              </div>
            </div>
            
            <div className="bg-white border border-gray-200 rounded-lg p-6">
              <h4 className="font-semibold mb-4 flex items-center">
                <CogIcon className="h-5 w-5 mr-2" />
                Runtime Environment
              </h4>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Go Version</span>
                  <span className="font-medium">{data.environment.goVersion}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">K6 Version</span>
                  <span className="font-medium">{data.environment.k6Version}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Network Type</span>
                  <span className="font-medium">{data.environment.networkType}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-sm text-gray-600">Region</span>
                  <span className="font-medium">{data.environment.region}</span>
                </div>
              </div>
            </div>
          </div>
        </ExpandableSection>
      )}
      
      {/* Performance Recommendations */}
      {recommendations.length > 0 && (
        <ExpandableSection 
          title={`Performance Recommendations (${recommendations.length})`}
          defaultExpanded={false}
        >
          <div className="space-y-4">
            {recommendations.map((recommendation, index) => (
              <div key={index} className="bg-white border border-gray-200 rounded-lg p-6">
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center mb-2">
                      <div className={`w-2 h-2 rounded-full mr-3 ${
                        recommendation.priority === 'critical' ? 'bg-red-500' :
                        recommendation.priority === 'high' ? 'bg-orange-500' :
                        recommendation.priority === 'medium' ? 'bg-yellow-500' : 'bg-green-500'
                      }`}></div>
                      <h4 className="font-semibold text-gray-900">{recommendation.title}</h4>
                      <span className={`ml-2 px-2 py-1 text-xs rounded-full ${
                        recommendation.priority === 'critical' ? 'bg-red-100 text-red-800' :
                        recommendation.priority === 'high' ? 'bg-orange-100 text-orange-800' :
                        recommendation.priority === 'medium' ? 'bg-yellow-100 text-yellow-800' : 'bg-green-100 text-green-800'
                      }`}>
                        {recommendation.priority}
                      </span>
                    </div>
                    <p className="text-gray-600 mb-3">{recommendation.description}</p>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
                      <div>
                        <span className="font-medium text-gray-900">Impact:</span>
                        <span className="text-gray-600 ml-2">{recommendation.impact}</span>
                      </div>
                      <div>
                        <span className="font-medium text-gray-900">Estimated Improvement:</span>
                        <span className="text-green-600 ml-2">{recommendation.estimatedImprovement}%</span>
                      </div>
                    </div>
                  </div>
                  <div className="ml-4">
                    <LightBulbIcon className="h-6 w-6 text-yellow-500" />
                  </div>
                </div>
                <div className="mt-4 bg-gray-50 rounded-lg p-3">
                  <h5 className="font-medium text-gray-900 mb-2">Recommended Action</h5>
                  <p className="text-sm text-gray-600">{recommendation.action}</p>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {recommendation.resources.map((resource, resourceIndex) => (
                      <span 
                        key={resourceIndex}
                        className="px-2 py-1 bg-blue-100 text-blue-800 text-xs rounded"
                      >
                        {resource}
                      </span>
                    ))}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </ExpandableSection>
      )}
      
      {/* Capacity Analysis */}
      <ExpandableSection title="Capacity Analysis" defaultExpanded={false}>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <div className="bg-white border border-gray-200 rounded-lg p-6">
            <h4 className="font-semibold mb-4 flex items-center">
              <ScaleIcon className="h-5 w-5 mr-2" />
              Current Capacity
            </h4>
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <span className="text-sm text-gray-600">Current Usage</span>
                <span className="font-medium">{systemMetrics.capacity.current.toFixed(1)}%</span>
              </div>
              <div className="w-full bg-gray-200 rounded-full h-2">
                <div 
                  className={`h-2 rounded-full ${
                    capacityUtilization > 90 ? 'bg-red-500' :
                    capacityUtilization > 70 ? 'bg-orange-500' :
                    capacityUtilization > 50 ? 'bg-yellow-500' : 'bg-green-500'
                  }`}
                  style={{ width: `${Math.min(capacityUtilization, 100)}%` }}
                ></div>
              </div>
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Maximum Capacity</span>
                <span className="font-medium">{systemMetrics.capacity.maximum.toFixed(1)}%</span>
              </div>
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Projected Usage</span>
                <span className="font-medium">{systemMetrics.capacity.projected.toFixed(1)}%</span>
              </div>
            </div>
          </div>
          
          <div className="bg-white border border-gray-200 rounded-lg p-6">
            <h4 className="font-semibold mb-4 flex items-center">
              <ArrowTrendingUpIcon className="h-5 w-5 mr-2" />
              Growth Projections
            </h4>
            <div className="space-y-3">
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Growth Rate</span>
                <span className="font-medium">{systemMetrics.capacity.growthRate.toFixed(1)}%/week</span>
              </div>
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Time to Capacity</span>
                <span className={`font-medium ${
                  timeToCapacity < 30 ? 'text-red-600' :
                  timeToCapacity < 90 ? 'text-orange-600' : 'text-green-600'
                }`}>
                  {timeToCapacity} days
                </span>
              </div>
              <div className="mt-4 bg-gray-50 rounded-lg p-3">
                <h5 className="font-medium text-gray-900 mb-2">Capacity Recommendations</h5>
                <ul className="space-y-1">
                  {systemMetrics.capacity.recommendations.map((rec, index) => (
                    <li key={index} className="text-sm text-gray-600">• {rec}</li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
        </div>
      </ExpandableSection>
    </div>
  )
}
import {
  TrendChart,
  MetricCard,
  LatencyCard,
  SuccessRateCard,
  ThroughputCard,
  LatencyDistribution,
  ErrorRateChart,
  ThroughputChart,
} from './index'
import type {
  TrendPoint,
  LatencyBucket,
  ErrorDataPoint,
  ThroughputDataPoint,
  Regression,
} from './index'

// Sample data generators
const generateTrendData = (days = 7): TrendPoint[] => {
  const data: TrendPoint[] = []
  const now = new Date()
  
  for (let i = days; i >= 0; i--) {
    const timestamp = new Date(now.getTime() - i * 24 * 60 * 60 * 1000).toISOString()
    
    // Generate multiple percentiles
    const percentiles = ['p50', 'p90', 'p95', 'p99'] as const
    percentiles.forEach((percentile) => {
      const baseLatency = percentile === 'p50' ? 100 : 
                         percentile === 'p90' ? 200 : 
                         percentile === 'p95' ? 300 : 500
      
      data.push({
        timestamp,
        value: baseLatency + Math.random() * 50 - 25,
        runId: `run-${i}-${percentile}`,
        metadata: {
          percentile,
          gitCommit: `abc123${i}`,
          gitBranch: 'main',
          testName: 'eth_getBalance',
        },
      })
    })
  }
  
  return data
}

const generateLatencyDistribution = (): LatencyBucket[] => {
  const buckets = [
    { min: 0, max: 50, count: 1500, percentage: 15 },
    { min: 50, max: 100, count: 3000, percentage: 30 },
    { min: 100, max: 150, count: 2500, percentage: 25 },
    { min: 150, max: 200, count: 1500, percentage: 15 },
    { min: 200, max: 250, count: 800, percentage: 8 },
    { min: 250, max: 300, count: 400, percentage: 4 },
    { min: 300, max: 500, count: 200, percentage: 2 },
    { min: 500, max: 1000, count: 100, percentage: 1 },
  ]
  return buckets
}

const generateErrorData = (days = 7): ErrorDataPoint[] => {
  const data: ErrorDataPoint[] = []
  const now = new Date()
  
  for (let i = days; i >= 0; i--) {
    const timestamp = new Date(now.getTime() - i * 24 * 60 * 60 * 1000).toISOString()
    const totalRequests = 1000 + Math.random() * 500
    const totalErrors = Math.floor(totalRequests * (0.01 + Math.random() * 0.05))
    
    data.push({
      timestamp,
      errorRate: totalErrors / totalRequests,
      totalErrors,
      totalRequests: Math.floor(totalRequests),
      runId: `run-${i}`,
      errors: [
        { error: 'Connection timeout', count: Math.floor(totalErrors * 0.4), percentage: 40 },
        { error: 'Rate limit exceeded', count: Math.floor(totalErrors * 0.3), percentage: 30 },
        { error: 'Invalid request', count: Math.floor(totalErrors * 0.2), percentage: 20 },
        { error: 'Server error', count: Math.floor(totalErrors * 0.1), percentage: 10 },
      ],
    })
  }
  
  return data
}

const generateThroughputData = (days = 7): ThroughputDataPoint[] => {
  const data: ThroughputDataPoint[] = []
  const now = new Date()
  
  for (let i = days; i >= 0; i--) {
    const timestamp = new Date(now.getTime() - i * 24 * 60 * 60 * 1000).toISOString()
    const throughput = 800 + Math.random() * 400
    const duration = 60 + Math.random() * 30
    
    data.push({
      timestamp,
      throughput,
      totalRequests: Math.floor(throughput * duration),
      duration,
      runId: `run-${i}`,
      metadata: {
        client: ['geth', 'nethermind', 'besu'][Math.floor(Math.random() * 3)],
        method: 'eth_getBalance',
        testName: 'Load Test',
      },
    })
  }
  
  return data
}

const sampleRegressions: Regression[] = [
  {
    runId: 'run-3',
    baselineId: 'baseline-1',
    metricName: 'p95_latency',
    baselineValue: 280,
    currentValue: 350,
    percentChange: 25,
    severity: 'major',
  },
]

const sampleDeployments = [
  {
    timestamp: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
    version: 'v1.2.3',
    description: 'Performance improvements',
  },
  {
    timestamp: new Date(Date.now() - 1 * 24 * 60 * 60 * 1000).toISOString(),
    version: 'v1.2.4',
    description: 'Bug fixes',
  },
]

/**
 * Component Examples - Storybook-style demonstrations
 * 
 * This file showcases all the visualization components with sample data
 * to help developers understand how to use them.
 */
export function ComponentExamples() {
  const trendData = generateTrendData()
  const latencyBuckets = generateLatencyDistribution()
  const errorData = generateErrorData()
  const throughputData = generateThroughputData()

  return (
    <div className="space-y-8 p-6">
      <div className="text-center mb-8">
        <h1 className="text-3xl font-bold text-gray-900 mb-4">
          Visualization Components Gallery
        </h1>
        <p className="text-lg text-gray-600">
          Interactive examples of all available visualization components
        </p>
      </div>

      {/* Metric Cards Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Metric Cards</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <MetricCard
            title="Average Latency"
            value={156.7}
            unit="ms"
            previousValue={143.2}
            percentageChange={9.4}
            trend="up"
            trendLabel="vs last week"
          />
          
          <LatencyCard
            title="P95 Latency"
            value={285.3}
            previousValue={268.1}
            percentageChange={6.4}
            trend="up"
            variant="warning"
          />
          
          <SuccessRateCard
            title="Success Rate"
            value={0.996}
            previousValue={0.994}
            percentageChange={0.2}
            trend="up"
          />
          
          <ThroughputCard
            title="Throughput"
            value={1250}
            previousValue={1180}
            percentageChange={5.9}
            trend="up"
          />
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          {/* Different sizes */}
          <MetricCard
            title="Small Card"
            value={42}
            size="sm"
            variant="success"
          />
          
          <MetricCard
            title="Medium Card"
            value={1337}
            size="md"
            loading={false}
          />
          
          <MetricCard
            title="Large Card"
            value={9001}
            size="lg"
            variant="danger"
            subtitle="Over 9000!"
          />
        </div>
      </section>

      {/* Trend Chart Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Trend Chart</h2>
        <TrendChart
          data={trendData}
          title="Latency Trends by Percentile"
          metric="Latency (ms)"
          showBaseline={true}
          baselineData={trendData.filter(p => p.metadata?.percentile === 'p50')}
          regressions={sampleRegressions}
          deployments={sampleDeployments}
          onPointClick={(point) => {
            console.log('Clicked point:', point)
            alert(`Clicked: ${point.metadata?.percentile} at ${point.timestamp}`)
          }}
        />
      </section>

      {/* Latency Distribution Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Latency Distribution</h2>
        <LatencyDistribution
          data={latencyBuckets}
          title="Request Latency Distribution"
          showPercentiles={true}
          percentiles={{
            p50: 125,
            p90: 245,
            p95: 315,
            p99: 580,
          }}
          showStatistics={true}
          bucketSize={50}
          onBucketClick={(bucket) => {
            console.log('Clicked bucket:', bucket)
            alert(`Clicked bucket: ${bucket.min}-${bucket.max}ms (${bucket.count} requests)`)
          }}
        />
      </section>

      {/* Error Rate Chart Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Error Rate Chart</h2>
        <ErrorRateChart
          data={errorData}
          title="Error Rate Over Time"
          showThreshold={true}
          errorThreshold={0.05}
          showErrorTypes={true}
          onPointClick={(point) => {
            console.log('Clicked error point:', point)
            alert(`Error rate: ${(point.errorRate * 100).toFixed(2)}% at ${point.timestamp}`)
          }}
        />
      </section>

      {/* Throughput Chart Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Throughput Chart</h2>
        <ThroughputChart
          data={throughputData}
          title="Request Throughput Over Time"
          showTarget={true}
          targetThroughput={1000}
          showTrend={true}
          groupBy="time"
          onBarClick={(point) => {
            console.log('Clicked throughput bar:', point)
            alert(`Throughput: ${point.throughput.toFixed(0)} req/s at ${point.timestamp}`)
          }}
        />
      </section>

      {/* Grouped Charts Section */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Grouped Charts</h2>
        
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <ErrorRateChart
            data={errorData}
            title="Error Rate by Client"
            groupBy="client"
            height={300}
          />
          
          <ThroughputChart
            data={throughputData}
            title="Throughput by Client"
            groupBy="client"
            height={300}
          />
        </div>
      </section>

      {/* Loading and Error States */}
      <section>
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Loading & Error States</h2>
        
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <TrendChart
            data={[]}
            title="Loading State"
            metric="Latency (ms)"
            loading={true}
            height={300}
          />
          
          <TrendChart
            data={[]}
            title="Error State"
            metric="Latency (ms)"
            error="Failed to load data from API"
            height={300}
          />
        </div>
      </section>

      {/* Usage Instructions */}
      <section className="bg-gray-50 p-6 rounded-lg">
        <h2 className="text-2xl font-semibold text-gray-900 mb-4">Usage Instructions</h2>
        
        <div className="prose prose-gray max-w-none">
          <h3>Importing Components</h3>
          <pre className="bg-gray-800 text-green-400 p-4 rounded text-sm overflow-x-auto">
{`import {
  TrendChart,
  MetricCard,
  LatencyCard,
  SuccessRateCard,
  ThroughputCard,
  LatencyDistribution,
  ErrorRateChart,
  ThroughputChart,
} from './components'`}
          </pre>

          <h3>Key Features</h3>
          <ul className="list-disc list-inside space-y-2">
            <li><strong>Interactive Charts:</strong> All charts support zoom, pan, and click interactions</li>
            <li><strong>Export Functionality:</strong> Charts can be exported as PNG images</li>
            <li><strong>Responsive Design:</strong> Components adapt to different screen sizes</li>
            <li><strong>Accessibility:</strong> ARIA labels and keyboard navigation support</li>
            <li><strong>TypeScript:</strong> Full type safety with comprehensive interfaces</li>
            <li><strong>Loading States:</strong> Built-in loading spinners and error handling</li>
            <li><strong>Customizable:</strong> Flexible styling with Tailwind CSS classes</li>
          </ul>

          <h3>Chart Interactions</h3>
          <ul className="list-disc list-inside space-y-1">
            <li>Drag to pan the view</li>
            <li>Scroll or pinch to zoom</li>
            <li>Click data points for details</li>
            <li>Hover for tooltips</li>
            <li>Use toolbar buttons for zoom controls</li>
          </ul>
        </div>
      </section>
    </div>
  )
}

export default ComponentExamples
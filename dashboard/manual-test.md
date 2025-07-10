# Manual Testing Guide for JSON-RPC Benchmark Dashboard

This guide provides a manual approach to test the dashboard implementation.

## Prerequisites

1. **Start the API Server** (if available):
   ```bash
   cd runner
   go run main.go api --port 8082
   ```

2. **Start the Dashboard Dev Server**:
   ```bash
   cd dashboard
   npm run dev
   ```

3. Open browser at http://localhost:3000

## Test Scenarios

### 1. Dashboard Page Tests

**Navigation**: Go to http://localhost:3000

**Expected Behavior**:
- ✅ Page loads with "Performance Dashboard" title
- ✅ Stats cards show:
  - Total Runs
  - Average Success Rate
  - Average Latency
  - Total Clients
- ✅ Recent Runs table displays with columns:
  - Test ID
  - Client Version
  - Success Rate
  - Avg Latency
  - Throughput
  - Timestamp
  - Actions (View Details)
- ✅ Charts display:
  - Success Rate Trend
  - Latency Trend

### 2. Run Details Page Tests

**Navigation**: Click "View Details" on any run from the dashboard

**Expected Behavior**:
- ✅ Breadcrumb shows: Dashboard > Run Details > [Run ID]
- ✅ Run information displays:
  - Run ID, Status, Client Version
  - Start/End Time
  - Total Duration
  - Configuration details
- ✅ Performance metrics cards show
- ✅ Charts display:
  - Latency Distribution
  - Success Rate Over Time
  - Throughput Over Time
  - Error Rate by Method
- ✅ Methods table shows all RPC methods with metrics

### 3. Compare Page Tests

**Navigation**: Go to http://localhost:3000/compare

**Expected Behavior**:
- ✅ Page title shows "Compare Benchmark Runs"
- ✅ Two dropdown selectors for baseline and target runs
- ✅ Filter options for branch and sorting
- ✅ After selecting two runs and clicking "Compare":
  - Comparison summary shows improvements/degradations
  - Side-by-side metrics comparison
  - Comparison charts for:
    - Latency
    - Throughput
    - Success Rate
    - Error Rate
  - Detailed method comparison table

### 4. Baselines Page Tests

**Navigation**: Go to http://localhost:3000/baselines

**Expected Behavior**:
- ✅ Page title shows "Baseline Management"
- ✅ "Add New Baseline" button is visible
- ✅ Baselines table shows:
  - Name
  - Description
  - Git Info (commit/branch)
  - Performance metrics
  - Created date
  - Actions (Set as Default, Delete)
- ✅ Add baseline flow:
  - Click "Add New Baseline"
  - Form appears with fields
  - Select a run
  - Enter name and description
  - Save creates new baseline

### 5. Error Handling Tests

**Test**: Disconnect API server and refresh pages

**Expected Behavior**:
- ✅ Error messages display gracefully
- ✅ Retry buttons appear
- ✅ No crashes or blank screens

### 6. Responsive Design Tests

**Test**: Resize browser window to mobile size (< 768px)

**Expected Behavior**:
- ✅ Navigation menu collapses
- ✅ Tables become scrollable
- ✅ Charts resize appropriately
- ✅ Cards stack vertically
- ✅ All content remains accessible

### 7. Real-time Updates Test

**Test**: If WebSocket is enabled, create new runs

**Expected Behavior**:
- ✅ Dashboard updates automatically with new runs
- ✅ Charts refresh with new data
- ✅ No page refresh required

## Manual Test Results

Run through each test scenario and mark the checkboxes:

| Feature | Status | Notes |
|---------|--------|-------|
| Dashboard Stats | ⬜ | |
| Recent Runs Table | ⬜ | |
| Charts Display | ⬜ | |
| Run Details Page | ⬜ | |
| Compare Functionality | ⬜ | |
| Baseline Management | ⬜ | |
| Error Handling | ⬜ | |
| Responsive Design | ⬜ | |
| Real-time Updates | ⬜ | |

## Quick Visual Test

You can also run a quick visual test by starting the dev server with mock data:

```bash
cd dashboard
npm run dev
```

Then check:
1. All pages load without errors
2. UI components render correctly
3. Interactions work (clicks, form submissions)
4. Data displays properly (even if mocked)

## Browser Console Check

Open browser DevTools (F12) and check for:
- No JavaScript errors in console
- No failed network requests (unless API is not running)
- No React warnings about keys or props
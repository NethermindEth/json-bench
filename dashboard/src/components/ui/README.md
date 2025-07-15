# UI Components

This directory contains reusable UI components for the JSON-RPC benchmark dashboard. These components are production-ready with full TypeScript support, accessibility features, and consistent styling.

## Components

### ExpandableSection

A flexible expandable/collapsible section component with smooth animations and various states.

**Features:**
- Smooth expand/collapse animations
- Loading and error states
- Header actions support
- Multiple variants (default, success, warning, danger)
- Accessibility features (ARIA attributes, keyboard navigation)
- Customizable sizes (sm, md, lg)

**Example:**
```tsx
<ExpandableSection
  title="Performance Metrics"
  subtitle="Click to expand"
  defaultExpanded={true}
  variant="success"
  headerActions={<button className="btn btn-primary">Action</button>}
>
  <p>Content goes here...</p>
</ExpandableSection>
```

### MetricTable

An enhanced table component with sorting, filtering, pagination, and export capabilities.

**Features:**
- Sortable columns with visual indicators
- Global and column-specific filtering
- Pagination for large datasets
- Row selection (single or multiple)
- Export functionality
- Custom column rendering
- Responsive design
- Accessibility features

**Example:**
```tsx
const columns: ColumnDef<DataType>[] = [
  {
    id: 'name',
    header: 'Name',
    accessor: 'name',
    sortable: true,
    filterable: true,
  },
  {
    id: 'value',
    header: 'Value',
    accessor: 'value',
    render: (value) => `${value.toFixed(2)}ms`,
  },
]

<MetricTable
  data={data}
  columns={columns}
  title="Performance Data"
  sortable={true}
  filterable={true}
  exportable={true}
  selectable={true}
  onRowClick={(row) => console.log('Row clicked:', row)}
/>
```

### ExportButton

A versatile export button component supporting multiple formats with progress indicators.

**Features:**
- Multiple export formats (JSON, CSV, Excel, Text)
- Progress indicators for large exports
- Custom export handlers
- Dropdown for multiple formats
- Single button for single format
- Loading states and error handling

**Example:**
```tsx
<ExportButton
  data={tableData}
  filename="performance_report"
  formats={['json', 'csv', 'xlsx']}
  variant="primary"
  onExport={(format, data) => {
    // Custom export logic
  }}
/>
```

## Styling

All components use the existing design system with:
- Tailwind CSS classes
- Consistent color palette (primary, success, warning, danger)
- Responsive breakpoints
- Smooth animations and transitions
- Accessibility-first approach

## Integration

Import components from the main components index:

```tsx
import { ExpandableSection, MetricTable, ExportButton } from '../components'
```

Or directly from the UI components:

```tsx
import { ExpandableSection, MetricTable, ExportButton } from '../components/ui'
```

## Accessibility

All components include:
- Proper ARIA attributes
- Keyboard navigation support
- Screen reader compatibility
- Focus management
- Semantic HTML structure

## Browser Support

Components are tested and compatible with:
- Chrome 90+
- Firefox 88+
- Safari 14+
- Edge 90+
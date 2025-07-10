# JSON-RPC Benchmark Dashboard

A modern React dashboard for tracking JSON-RPC benchmark performance over time. Part of the historic tracking system for the JSON-RPC benchmarking tool.

## Features

- **Performance Tracking**: View historic benchmark results and trends
- **Run Comparison**: Side-by-side comparison of benchmark runs
- **Baseline Management**: Set and manage performance baselines
- **Real-time Updates**: WebSocket integration for live updates
- **Responsive Design**: Works on desktop and mobile devices
- **Chart Visualizations**: Interactive charts powered by Chart.js

## Tech Stack

- **React 18** with TypeScript
- **Vite** for fast development and building
- **Tailwind CSS** for styling
- **React Router** for navigation
- **TanStack Query** for API state management
- **Chart.js** + React-Chartjs-2 for visualizations
- **Axios** for HTTP requests
- **Date-fns** for date manipulation

## Getting Started

### Prerequisites

- Node.js 18+ 
- npm or yarn
- Running JSON-RPC benchmark backend API (port 8080)

### Installation

1. **Install dependencies:**
   ```bash
   npm install
   ```

2. **Start development server:**
   ```bash
   npm run dev
   ```
   
   The dashboard will be available at `http://localhost:3000`

3. **For production build:**
   ```bash
   npm run build
   npm run preview
   ```

### Environment Configuration

The dashboard is configured to connect to the backend API at `http://localhost:8080`. This can be modified in `vite.config.ts`:

```typescript
server: {
  proxy: {
    '/api': {
      target: 'http://your-backend-host:8080',
      changeOrigin: true,
    },
  },
}
```

## Project Structure

```
dashboard/
├── src/
│   ├── components/          # Reusable UI components
│   │   ├── Layout.tsx       # Main layout with navigation
│   │   └── LoadingSpinner.tsx
│   ├── pages/               # Page components
│   │   ├── Dashboard.tsx    # Main dashboard page
│   │   ├── RunDetails.tsx   # Individual run details
│   │   ├── Compare.tsx      # Run comparison page
│   │   ├── Baselines.tsx    # Baseline management
│   │   └── NotFound.tsx     # 404 page
│   ├── api/                 # API client (to be implemented)
│   ├── App.tsx              # Main app component with routing
│   ├── main.tsx             # App entry point
│   └── index.css            # Global styles and Tailwind imports
├── public/                  # Static assets
├── package.json             # Dependencies and scripts
├── tsconfig.json            # TypeScript configuration
├── vite.config.ts           # Vite configuration
├── tailwind.config.js       # Tailwind CSS configuration
└── README.md               # This file
```

## Development Scripts

- `npm run dev` - Start development server with hot reload
- `npm run build` - Build for production
- `npm run preview` - Preview production build locally
- `npm run lint` - Run ESLint
- `npm run type-check` - Run TypeScript type checking

## API Integration

The dashboard expects the following API endpoints to be available:

### Runs
- `GET /api/runs` - List historic runs
- `GET /api/runs/:id` - Get specific run details
- `GET /api/runs/:id/report` - Get full benchmark result

### Trends
- `GET /api/trends` - Get trend data
- `GET /api/trends/method/:name` - Method-specific trends
- `GET /api/trends/client/:name` - Client-specific trends

### Baselines
- `GET /api/baselines` - List baselines
- `POST /api/runs/:id/baseline` - Set run as baseline

### Comparisons
- `GET /api/compare?run1=:id1&run2=:id2` - Compare two runs
- `GET /api/regressions/:id` - Get regression report

### WebSocket
- `WS /api/ws` - Real-time updates

## Customization

### Styling
The dashboard uses Tailwind CSS with a custom color palette. Modify `tailwind.config.js` to customize colors, spacing, and other design tokens.

### Components
All components are built with accessibility in mind and use semantic HTML. The design system includes:

- Consistent button styles (`.btn`, `.btn-primary`, etc.)
- Card layouts (`.card`, `.card-header`, `.card-content`)
- Table styles (`.table`, `.table-row`, etc.)
- Badge components (`.badge`, `.badge-success`, etc.)

### Charts
Chart.js integration is ready for implementation. The placeholder components in the pages show where interactive charts will be added.

## Production Deployment

### Build Optimization
The Vite configuration includes:
- Code splitting for vendor libraries
- Source maps for debugging
- Optimized chunk sizes

### Deployment Options
1. **Static Hosting**: Deploy the `dist/` folder to any static host
2. **Docker**: Use the included Dockerfile for containerized deployment
3. **CDN**: Upload to CDN for global distribution

### Environment Variables
For production, you may want to configure:
- `VITE_API_URL` - Backend API base URL
- `VITE_WS_URL` - WebSocket endpoint URL

## Browser Support

- Chrome 90+
- Firefox 88+
- Safari 14+
- Edge 90+

## Contributing

1. Follow the existing code style and TypeScript conventions
2. Use the component library patterns for consistency
3. Ensure all new components are accessible
4. Add proper error handling and loading states
5. Test on multiple screen sizes

## License

This project is part of the JSON-RPC benchmarking tool and follows the same license.
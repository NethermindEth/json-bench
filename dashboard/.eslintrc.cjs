module.exports = {
  root: true,
  env: { 
    browser: true, 
    es2020: true,
    node: true // Added for test files and config files
  },
  globals: {
    JSX: 'readonly'
  },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:react/recommended',
    'plugin:react-hooks/recommended'
  ],
  ignorePatterns: ['dist', '.eslintrc.cjs', 'node_modules', 'playwright-report', 'test-results'],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module',
    ecmaFeatures: {
      jsx: true
    }
  },
  plugins: ['react-refresh', '@typescript-eslint', 'react-hooks', 'react'],
  settings: {
    react: {
      version: 'detect'
    }
  },
  rules: {
    'react-refresh/only-export-components': [
      'warn',
      { allowConstantExport: true },
    ],
    '@typescript-eslint/no-unused-vars': ['error', { 
      argsIgnorePattern: '^_',
      varsIgnorePattern: '^_',
      ignoreRestSiblings: true
    }],
    '@typescript-eslint/no-explicit-any': 'warn',
    'react-hooks/exhaustive-deps': 'warn',
    'no-unused-vars': 'off', // Handled by @typescript-eslint/no-unused-vars
    'react/react-in-jsx-scope': 'off', // Not needed with React 17+
    'react/prop-types': 'off', // Using TypeScript for prop validation
    'no-undef': 'error',
    'no-redeclare': 'off', // Handled by TypeScript
    '@typescript-eslint/no-redeclare': 'error'
  },
  overrides: [
    {
      // Test files
      files: ['**/*.test.ts', '**/*.test.tsx', '**/test/**', '**/tests/**', 'src/test/**'],
      env: {
        jest: true,
        node: true
      },
      rules: {
        'react-refresh/only-export-components': 'off'
      }
    },
    {
      // Config files
      files: ['*.config.ts', '*.config.js', 'vite.config.ts', 'playwright.config.ts', 'playwright.docker.config.ts'],
      env: {
        node: true
      }
    }
  ]
}
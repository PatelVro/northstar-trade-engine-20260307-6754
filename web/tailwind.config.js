/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      // Cirelay design tokens — financial-ops dark theme.
      // Names follow semantic role so we can retheme without touching components.
      colors: {
        bg: {
          base: '#0A0C10',
          surface: '#13161C',
          elevated: '#1A1F29',
          hover: '#232938',
        },
        border: {
          subtle: '#232A36',
          strong: '#2E3745',
        },
        brand: {
          50: '#EEF4FF',
          100: '#DCE8FF',
          200: '#B9D0FF',
          300: '#94B6FF',
          400: '#7AAFFF',
          500: '#4F8CFF',
          600: '#3A6EDB',
          700: '#2952B0',
          800: '#1E3E87',
          900: '#152B5E',
        },
        profit: {
          DEFAULT: '#16C784',
          soft: 'rgba(22, 199, 132, 0.12)',
        },
        loss: {
          DEFAULT: '#EA3943',
          soft: 'rgba(234, 57, 67, 0.12)',
        },
        warn: {
          DEFAULT: '#F7B955',
          soft: 'rgba(247, 185, 85, 0.12)',
        },
        fg: {
          primary: '#E6EAF2',
          secondary: '#8A94A6',
          muted: '#5A6478',
        },
      },
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'sans-serif'],
        mono: ['JetBrains Mono', 'IBM Plex Mono', 'ui-monospace', 'monospace'],
      },
      fontSize: {
        'data': ['0.875rem', { lineHeight: '1.25rem', fontFeatureSettings: '"tnum"' }],
      },
      boxShadow: {
        'card': '0 1px 2px 0 rgba(0, 0, 0, 0.3)',
        'card-hover': '0 4px 12px -1px rgba(0, 0, 0, 0.5)',
        'glow-brand': '0 0 24px rgba(79, 140, 255, 0.15)',
        'glow-profit': '0 0 24px rgba(22, 199, 132, 0.15)',
        'glow-loss': '0 0 24px rgba(234, 57, 67, 0.15)',
      },
      animation: {
        'pulse-subtle': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'fade-in': 'fadeIn 0.3s ease-in-out',
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: 0, transform: 'translateY(4px)' },
          '100%': { opacity: 1, transform: 'translateY(0)' },
        },
      },
    },
  },
  plugins: [],
}

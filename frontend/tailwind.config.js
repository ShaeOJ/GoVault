/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}', './index.html'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        navy: {
          900: '#0a0e1a',
          800: '#111827',
          700: '#1a2035',
          600: '#232b3e',
        },
        electric: {
          300: '#93c5fd',
          400: '#60a5fa',
          500: '#3b82f6',
          600: '#2563eb',
        },
        mining: {
          gold: '#f59e0b',
          green: '#22c55e',
          red: '#ef4444',
        },
      },
      fontFamily: {
        tech: ['Share Tech Mono', 'monospace'],
        data: ['JetBrains Mono', 'monospace'],
      },
      animation: {
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'slide-in-right': 'slideInRight 300ms ease-in-out',
        'slide-out-right': 'slideOutRight 300ms ease-in-out',
        'slide-in-up': 'slideInUp 300ms ease-out',
        'fade-in': 'fadeIn 200ms ease-out',
        'count-up': 'countUp 500ms ease-out',
        'confetti-fall': 'confettiFall 1.5s ease-out forwards',
      },
      keyframes: {
        slideInRight: {
          from: { transform: 'translateX(100%)', opacity: '0' },
          to: { transform: 'translateX(0)', opacity: '1' },
        },
        slideOutRight: {
          from: { transform: 'translateX(0)', opacity: '1' },
          to: { transform: 'translateX(100%)', opacity: '0' },
        },
        slideInUp: {
          from: { transform: 'translateY(10px)', opacity: '0' },
          to: { transform: 'translateY(0)', opacity: '1' },
        },
        fadeIn: {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        confettiFall: {
          '0%': { transform: 'translateY(-100%) rotate(0deg)', opacity: '1' },
          '100%': { transform: 'translateY(100vh) rotate(720deg)', opacity: '0' },
        },
      },
    },
  },
  plugins: [],
}

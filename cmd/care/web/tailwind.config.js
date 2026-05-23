/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bodh: {
          50: '#f5f7fa',
          100: '#e5ecf3',
          200: '#c9d6e3',
          400: '#7593b3',
          600: '#3c5a7a',
          700: '#2d4660',
          800: '#1f3247',
        },
      },
      fontFamily: {
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'monospace'],
      },
    },
  },
  plugins: [],
};

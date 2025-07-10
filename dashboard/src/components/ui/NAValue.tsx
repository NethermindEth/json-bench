interface NAValueProps {
  className?: string
  children?: React.ReactNode
}

/**
 * Component to display N/A values with consistent styling
 */
export function NAValue({ className = '', children = 'N/A' }: NAValueProps) {
  return (
    <span className={`text-gray-400 italic font-normal ${className}`}>
      {children}
    </span>
  )
}

export default NAValue